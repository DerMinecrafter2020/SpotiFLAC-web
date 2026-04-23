//go:build web

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/afkarxyz/SpotiFLAC/backend"
)

type rpcRequest struct {
	Method string            `json:"method"`
	Args   []json.RawMessage `json:"args"`
}

type rpcResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type appEvent struct {
	Name string      `json:"name"`
	Data interface{} `json:"data"`
}

type eventHub struct {
	mu          sync.RWMutex
	subscribers map[chan []byte]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subscribers: make(map[chan []byte]struct{})}
}

func (h *eventHub) subscribe() chan []byte {
	ch := make(chan []byte, 32)
	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *eventHub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	if _, ok := h.subscribers[ch]; ok {
		delete(h.subscribers, ch)
		close(ch)
	}
	h.mu.Unlock()
}

func (h *eventHub) emit(name string, payload ...interface{}) {
	var data interface{}
	switch len(payload) {
	case 0:
		data = nil
	case 1:
		data = payload[0]
	default:
		data = payload
	}

	encoded, err := json.Marshal(appEvent{Name: name, Data: data})
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subscribers {
		select {
		case ch <- encoded:
		default:
		}
	}
}

func decodeArg(raw json.RawMessage, target reflect.Type) (reflect.Value, error) {
	if target.Kind() == reflect.Ptr {
		value := reflect.New(target.Elem())
		if len(raw) == 0 {
			return value, nil
		}
		if err := json.Unmarshal(raw, value.Interface()); err != nil {
			return reflect.Value{}, err
		}
		return value, nil
	}

	value := reflect.New(target)
	if len(raw) == 0 {
		return value.Elem(), nil
	}
	if err := json.Unmarshal(raw, value.Interface()); err != nil {
		return reflect.Value{}, err
	}
	return value.Elem(), nil
}

func invokeAppMethod(app *App, methodName string, args []json.RawMessage) (interface{}, error) {
	if strings.TrimSpace(methodName) == "" {
		return nil, errors.New("method is required")
	}

	method := reflect.ValueOf(app).MethodByName(methodName)
	if !method.IsValid() {
		return nil, fmt.Errorf("unknown method: %s", methodName)
	}

	methodType := method.Type()
	if methodType.NumIn() != len(args) {
		return nil, fmt.Errorf("invalid arg count for %s: expected %d, got %d", methodName, methodType.NumIn(), len(args))
	}

	inputs := make([]reflect.Value, methodType.NumIn())
	for i := 0; i < methodType.NumIn(); i++ {
		decoded, err := decodeArg(args[i], methodType.In(i))
		if err != nil {
			return nil, fmt.Errorf("invalid arg %d for %s: %w", i, methodName, err)
		}
		inputs[i] = decoded
	}

	results := method.Call(inputs)
	if len(results) == 0 {
		return nil, nil
	}

	last := results[len(results)-1]
	if last.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		if !last.IsNil() {
			return nil, last.Interface().(error)
		}
		results = results[:len(results)-1]
	}

	switch len(results) {
	case 0:
		return nil, nil
	case 1:
		return results[0].Interface(), nil
	default:
		out := make([]interface{}, 0, len(results))
		for _, result := range results {
			out = append(out, result.Interface())
		}
		return out, nil
	}
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func main() {
	backend.AppVersion = "web"
	app := NewApp()
	hub := newEventHub()
	SetAppEventSink(hub.emit)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/rpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, rpcResponse{Error: "method not allowed"})
			return
		}

		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, rpcResponse{Error: "invalid request body"})
			return
		}

		result, err := invokeAppMethod(app, req.Method, req.Args)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, rpcResponse{Error: err.Error()})
			return
		}

		writeJSON(w, http.StatusOK, rpcResponse{Result: result})
	})

	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", http.StatusInternalServerError)
			return
		}

		ch := hub.subscribe()
		defer hub.unsubscribe(ch)

		fmt.Fprintf(w, "event: ready\ndata: {}\n\n")
		flusher.Flush()

		for {
			select {
			case <-r.Context().Done():
				return
			case payload := <-ch:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
				flusher.Flush()
			}
		}
	})

	distDir := filepath.Join("frontend", "dist")
	indexFile := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(indexFile); err == nil {
		fs := http.FileServer(http.Dir(distDir))
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}

			reqPath := strings.TrimPrefix(r.URL.Path, "/")
			if reqPath == "" {
				http.ServeFile(w, r, indexFile)
				return
			}

			path := filepath.Join(distDir, filepath.FromSlash(reqPath))
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				fs.ServeHTTP(w, r)
				return
			}

			http.ServeFile(w, r, indexFile)
		})
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("Frontend build not found. Run: cd frontend && pnpm install && pnpm build"))
		})
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	log.Printf("SpotiFLAC web server listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

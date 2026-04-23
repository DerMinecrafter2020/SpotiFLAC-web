package backend

const amazonMusicAPIBaseURL = "https://amazon.spotbye.qzz.io"

var defaultAmazonMusicAPIBaseURLs = []string{
	"https://amazon.spotbye.qzz.io",
	"https://amazon-a.spotbye.qzz.io",
	"https://amazon-b.spotbye.qzz.io",
	"https://amazon-c.spotbye.qzz.io",
}

var defaultQobuzStreamAPIBaseURLs = []string{
	"https://dab.yeet.su/api/stream?trackId=",
	"https://dabmusic.xyz/api/stream?trackId=",
	"https://qobuz.spotbye.qzz.io/api/track/",
	"https://qbz.afkarxyz.qzz.io/api/track/",
	"https://qobuz.squid.wtf/api/download-music?country=US&track_id=",
}

func GetQobuzStreamAPIBaseURLs() []string {
	return append([]string(nil), defaultQobuzStreamAPIBaseURLs...)
}

func GetAmazonMusicAPIBaseURL() string {
	return amazonMusicAPIBaseURL
}

func GetAmazonMusicAPIBaseURLs() []string {
	return append([]string(nil), defaultAmazonMusicAPIBaseURLs...)
}

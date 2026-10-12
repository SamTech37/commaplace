package auth

import "golang.org/x/oauth2"

// UseFakeGoogle points the OAuth flow at a test server for the duration of a test.
func UseFakeGoogle(tokenURL, userinfoURL string) (restore func()) {
	ep, ui := googleEndpoint, googleUserinfoURL
	googleEndpoint = oauth2.Endpoint{AuthURL: tokenURL, TokenURL: tokenURL}
	googleUserinfoURL = userinfoURL
	return func() { googleEndpoint, googleUserinfoURL = ep, ui }
}

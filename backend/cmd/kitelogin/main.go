// kitelogin performs the Kite Connect daily login flow: it prints the
// login URL, waits for the request_token, and exchanges it for the
// day's access token.
//
// Kite access tokens expire daily (~6 AM IST), so run this each trading
// morning and put the token in .env as KITE_ACCESS_TOKEN.
//
// Usage:
//
//	KITE_API_KEY=xxx KITE_API_SECRET=yyy go run ./cmd/kitelogin
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	kiteconnect "github.com/zerodha/gokiteconnect/v4"
)

func main() {
	apiKey := os.Getenv("KITE_API_KEY")
	apiSecret := os.Getenv("KITE_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		fmt.Println("set KITE_API_KEY and KITE_API_SECRET")
		os.Exit(1)
	}

	kc := kiteconnect.New(apiKey)
	fmt.Println("1. Open this URL and log in with your Zerodha account:")
	fmt.Println("   " + kc.GetLoginURL())
	fmt.Println("2. After login you are redirected to your registered redirect URL")
	fmt.Println("   with ?request_token=... in the address bar.")
	fmt.Print("3. Paste the request_token here: ")

	reader := bufio.NewReader(os.Stdin)
	requestToken, _ := reader.ReadString('\n')
	requestToken = strings.TrimSpace(requestToken)
	if requestToken == "" {
		fmt.Println("no request token provided")
		os.Exit(1)
	}

	session, err := kc.GenerateSession(requestToken, apiSecret)
	if err != nil {
		fmt.Println("session generation failed:", err)
		os.Exit(1)
	}
	fmt.Println("\nSuccess! Put this in your .env:")
	fmt.Println("KITE_ACCESS_TOKEN=" + session.AccessToken)
}

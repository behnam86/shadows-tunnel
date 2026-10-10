// Package doctor provides safe, read-only baseline diagnostics.
package doctor

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"time"
)

// Run prints a concise environment report without changing system configuration.
func Run() {
	fmt.Println("Shadows Tunnel doctor")
	fmt.Printf("OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Printf("Go runtime: %s\n", runtime.Version())
	fmt.Printf("CPU count: %d\n", runtime.NumCPU())
	if wd, err := os.Getwd(); err == nil {
		fmt.Printf("Working directory: %s\n", wd)
	}
	fmt.Printf("Time: %s\n", time.Now().Format(time.RFC3339))
	for _, item := range []struct{ network, address string }{{"tcp4", "127.0.0.1:0"}, {"tcp6", "[::1]:0"}} {
		ln, err := net.Listen(item.network, item.address)
		if err != nil {
			fmt.Printf("%s loopback: unavailable (%v)\n", item.network, err)
			continue
		}
		fmt.Printf("%s loopback: OK (%s)\n", item.network, ln.Addr())
		_ = ln.Close()
	}
	fmt.Println("Note: firewall, external routing, UDP, MTU, DNS, and systemd health checks are not implemented in Phase 2.")
}

package main

import (
	"context"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Own and reap network-setup children in Go. An async shell job inherited by
// exec would otherwise become an unreaped child of the long-running supervisor.
func configureWiFi(ctx context.Context, id string) {
	n, err := strconv.Atoi(id)
	if err != nil || n < 0 {
		log.Print("Invalid configured Wi-Fi network ID")
		return
	}
	attempt := 0
	for ctx.Err() == nil {
		attempt++
		call, cancel := context.WithTimeout(ctx, 2*time.Second)
		b, err := exec.CommandContext(call, "/system/bin/wpa_cli", "-p", "/data/misc/wifi/sockets", "select_network", id).CombinedOutput()
		cancel()
		accepted := false
		for _, line := range strings.Split(string(b), "\n") {
			if strings.TrimSpace(line) == "OK" {
				accepted = true
			}
		}
		if err == nil && accepted {
			call, cancel = context.WithTimeout(ctx, 2*time.Second)
			err = exec.CommandContext(call, "/system/bin/start", "dhcpcd-wlan0").Run()
			cancel()
			if err == nil {
				log.Print("Existing Wi-Fi profile selected; DHCP requested")
				return
			}
		}
		if attempt == 1 || attempt%30 == 0 {
			log.Print("Waiting for Wi-Fi control; local privacy/wake remain available")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

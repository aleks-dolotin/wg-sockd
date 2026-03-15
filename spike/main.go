package main

import (
	"fmt"
	"log"

	"golang.zx2c4.com/wireguard/wgctrl"
)

func main() {
	client, err := wgctrl.New()
	if err != nil {
		log.Fatalf("failed to create wgctrl client: %v", err)
	}
	defer client.Close()

	dev, err := client.Device("wg0")
	if err != nil {
		log.Fatalf("failed to get device wg0: %v", err)
	}

	fmt.Printf("Interface: %s\n", dev.Name)
	fmt.Printf("  PublicKey:   %s\n", dev.PublicKey)
	fmt.Printf("  ListenPort:  %d\n", dev.ListenPort)
	fmt.Printf("  Peers:       %d\n", len(dev.Peers))
	fmt.Println()

	for i, p := range dev.Peers {
		fmt.Printf("  Peer #%d:\n", i+1)
		fmt.Printf("    PublicKey:      %s\n", p.PublicKey)
		fmt.Printf("    Endpoint:       %v\n", p.Endpoint)
		fmt.Printf("    LastHandshake:  %v\n", p.LastHandshakeTime)
		fmt.Printf("    AllowedIPs:     %v\n", p.AllowedIPs)
		fmt.Printf("    RxBytes:        %d\n", p.ReceiveBytes)
		fmt.Printf("    TxBytes:        %d\n", p.TransmitBytes)
	}
}

package main

import "testing"

func TestCheckAddr(t *testing.T) {
	ok := map[string]int{"127.0.0.1:8337": 8337, "localhost:9000": 9000, "[::1]:8337": 8337}
	for addr, port := range ok {
		got, err := checkAddr(addr)
		if err != nil || got != port {
			t.Errorf("checkAddr(%q) = %d, %v", addr, got, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:8337", ":8337", "192.168.18.5:8337", "127.0.0.1:нет", "127.0.0.1"} {
		if _, err := checkAddr(addr); err == nil {
			t.Errorf("checkAddr(%q) должен отказать", addr)
		}
	}
}

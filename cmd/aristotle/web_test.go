package main

import (
	"strings"
	"testing"
)

func TestCheckAddr(t *testing.T) {
	ok := map[string]int{"127.0.0.1:8337": 8337, "localhost:9000": 9000, "[::1]:8337": 8337, "localhost:8337": 8337, "127.0.0.1:65535": 65535}
	for addr, port := range ok {
		got, err := checkAddr(addr)
		if err != nil || got != port {
			t.Errorf("checkAddr(%q) = %d, %v", addr, got, err)
		}
	}

	// Адрес на петле, но не из трёх имён, которые принимает guard (Host
	// 127.0.0.1, localhost или [::1] с этим портом): сервер слушал бы, а каждый
	// запрос получил бы 421. Такие адреса отказ должен ловить при запуске, и
	// сообщение должно назвать, что разрешено.
	notGuardHost := []string{
		"127.0.0.2:8337", "127.1.2.3:8337", "[::ffff:127.0.0.1]:8337", "[0:0:0:0:0:0:0:1]:8337",
		"0.0.0.0:8337", "[::]:8337", ":8337", "example.com:80", "192.168.18.5:8337",
	}
	for _, addr := range notGuardHost {
		_, err := checkAddr(addr)
		if err == nil {
			t.Errorf("checkAddr(%q) должен отказать", addr)
			continue
		}
		if msg := err.Error(); !strings.Contains(msg, "localhost, 127.0.0.1 и ::1") || !strings.Contains(msg, addr) {
			t.Errorf("checkAddr(%q): сообщение %q должно назвать адрес и разрешённые имена localhost, 127.0.0.1 и ::1", addr, msg)
		}
	}

	// Хост годный, а порт или форма адреса — нет.
	badForm := []string{
		"127.0.0.1", "localhost", "8337", "127.0.0.1:нет", "127.0.0.1:",
		"127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:-1", "localhost:0", "[::1]:65536",
	}
	for _, addr := range badForm {
		if _, err := checkAddr(addr); err == nil {
			t.Errorf("checkAddr(%q) должен отказать", addr)
		}
	}
}

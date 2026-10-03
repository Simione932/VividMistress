package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"pi-chat-gateway/internal/db"
	"pi-chat-gateway/internal/llm"
	"pi-chat-gateway/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dataDir := flag.String("data", "./data", "data directory for JSON storage")
	rolesDir := flag.String("roles", server.DefaultRolesDir, "directory containing role .txt files")
	model := flag.String("model", server.DefaultModel, "default Ollama model name")
	llmDump := flag.String("llm-dump", "", "if set, directory where raw LLM requests/responses are dumped")
	tlsAddr := flag.String("tls-addr", ":8443", "HTTPS listen address (phone mic over LAN). Empty = HTTP only")
	certFile := flag.String("cert", "./cert.pem", "TLS cert file (auto-created if missing)")
	keyFile := flag.String("key", "./key.pem", "TLS key file (auto-created if missing)")
	flag.Parse()

	// Ensure data directory exists.
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		log.Fatalf("failed to create data dir: %v", err)
	}

	store, err := db.NewJSONStore(*dataDir)
	if err != nil {
		log.Fatalf("failed to init store: %v", err)
	}

	llmClient := llm.NewClient()
	if *llmDump != "" {
		if err := os.MkdirAll(*llmDump, 0o755); err != nil {
			log.Fatalf("failed to create llm dump dir: %v", err)
		}
		llmClient.DumpDir = *llmDump
		log.Printf("llm dump enabled: %s", filepath.Clean(*llmDump))
	}

	cfg := server.DefaultConfig()
	cfg.RolesDir = *rolesDir
	cfg.Model = *model

	// HTTPS sidecar (phone mic over LAN) runs alongside HTTP by default.
	if *tlsAddr != "" {
		if *tlsAddr == *addr {
			log.Fatalf("-tls-addr (%s) must differ from -addr (%s): one is HTTP, one is HTTPS", *tlsAddr, *addr)
		}
		if err := server.EnsureSelfSignedCert(*certFile, *keyFile, nil); err != nil {
			log.Fatalf("failed to create TLS cert: %v", err)
		}
		go func() {
			log.Printf("Vivid Mistress HTTPS (mic for phones) on %s — open https://<this-PC-LAN-IP>%s then Advanced > Proceed.", *tlsAddr, *tlsAddr)
			if err := server.RunTLSWithConfig(*tlsAddr, store, llmClient, cfg, *certFile, *keyFile); err != nil {
				log.Fatalf("HTTPS server error: %v", err)
			}
		}()
	}

	log.Printf("Vivid Mistress listening on %s (data: %s, roles: %s, model: %s)",
		*addr, filepath.Clean(*dataDir), filepath.Clean(*rolesDir), *model)
	if *tlsAddr != "" {
		log.Printf("HTTPS also on %s (phone mic) — open https://<this-PC-LAN-IP>%s", *tlsAddr, *tlsAddr)
	}
	if err := server.RunWithConfig(*addr, store, llmClient, cfg); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

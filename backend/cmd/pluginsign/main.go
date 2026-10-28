package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
)

func main() {
	genKey := flag.Bool("generate-key", false, "Generate a new Ed25519 signing keypair")
	signFile := flag.String("sign", "", "Path to plugin archive (.zip or .tar.gz) to sign")
	verifyFile := flag.String("verify", "", "Path to plugin archive to verify")
	privKeyHex := flag.String("private-key", "", "Hex-encoded Ed25519 private key (64 bytes / 128 hex chars)")
	pubKeyHex := flag.String("public-key", "", "Hex-encoded Ed25519 public key (32 bytes / 64 hex chars)")
	sigFile := flag.String("sig-file", "", "Output/Input signature file path (defaults to <archive>.sig)")

	flag.Parse()

	if *genKey {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating keypair: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("=== New Ed25519 Plugin Signing Keypair ===")
		fmt.Printf("Public Key (register in Kraken): %s\n", hex.EncodeToString(pub))
		fmt.Printf("Private Key (KEEP SECRET!):      %s\n", hex.EncodeToString(priv))
		return
	}

	if *signFile != "" {
		if *privKeyHex == "" {
			fmt.Fprintln(os.Stderr, "Error: -private-key is required when signing")
			os.Exit(1)
		}
		privBytes, err := hex.DecodeString(*privKeyHex)
		if err != nil || len(privBytes) != ed25519.PrivateKeySize {
			fmt.Fprintf(os.Stderr, "Error: invalid private key (expected %d bytes, got %d): %v\n", ed25519.PrivateKeySize, len(privBytes), err)
			os.Exit(1)
		}

		data, err := os.ReadFile(*signFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading archive: %v\n", err)
			os.Exit(1)
		}

		sig := ed25519.Sign(privBytes, data)

		outPath := *sigFile
		if outPath == "" {
			outPath = *signFile + ".sig"
		}

		if err := os.WriteFile(outPath, sig, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing signature file: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Successfully signed '%s' -> '%s' (signature length: %d bytes)\n", *signFile, outPath, len(sig))
		return
	}

	if *verifyFile != "" {
		if *pubKeyHex == "" {
			fmt.Fprintln(os.Stderr, "Error: -public-key is required when verifying")
			os.Exit(1)
		}
		pubBytes, err := hex.DecodeString(*pubKeyHex)
		if err != nil || len(pubBytes) != ed25519.PublicKeySize {
			fmt.Fprintf(os.Stderr, "Error: invalid public key (expected %d bytes): %v\n", ed25519.PublicKeySize, err)
			os.Exit(1)
		}

		inPath := *sigFile
		if inPath == "" {
			inPath = *verifyFile + ".sig"
		}

		sigBytes, err := os.ReadFile(inPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading signature file: %v\n", err)
			os.Exit(1)
		}

		data, err := os.ReadFile(*verifyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading archive: %v\n", err)
			os.Exit(1)
		}

		if ed25519.Verify(pubBytes, data, sigBytes) {
			fmt.Println("SUCCESS: Signature is VALID.")
		} else {
			fmt.Fprintln(os.Stderr, "FAILED: Signature is INVALID!")
			os.Exit(1)
		}
		return
	}

	flag.Usage()
}

package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func EnsureSelfSigned(certPath, keyPath, publicIP string) (string, error) {
	if b, err := os.ReadFile(certPath); err == nil {
		blk, _ := pem.Decode(b)
		if blk != nil {
			if cert, err := x509.ParseCertificate(blk.Bytes); err == nil {
				return Fingerprint(cert.Raw), nil
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0750); err != nil {
		return "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, _ := rand.Int(rand.Reader, serialLimit)
	tpl := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "MarzWatch Central"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, DNSNames: []string{"localhost"}}
	if ip := net.ParseIP(publicIP); ip != nil {
		tpl.IPAddresses = []net.IP{ip, net.ParseIP("127.0.0.1")}
	} else {
		tpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	cb := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	kp := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	if err := os.WriteFile(certPath, cb, 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(keyPath, kp, 0600); err != nil {
		return "", err
	}
	return Fingerprint(der), nil
}

func Fingerprint(der []byte) string { s := sha256.Sum256(der); return hex.EncodeToString(s[:]) }

func VerifyFingerprint(expected string) func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("no server certificate")
		}
		if Fingerprint(rawCerts[0]) != expected {
			return fmt.Errorf("central certificate fingerprint mismatch")
		}
		return nil
	}
}

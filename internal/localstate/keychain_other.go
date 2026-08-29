//go:build !darwin

package localstate

import (
	"context"
)

type stubKeychainReader struct{}

// NewPlatformKeychainReader returns a stub KeychainReader returning ErrKeychainUnsupported.
func NewPlatformKeychainReader() KeychainReader {
	return &stubKeychainReader{}
}

func (r *stubKeychainReader) GetGenericPassword(ctx context.Context, service string, account string) ([]byte, error) {
	return nil, ErrKeychainUnsupported
}

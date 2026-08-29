package localstate

import (
	"context"
	"errors"
)

var (
	// ErrKeychainUnsupported is returned when Keychain access is not supported on the current platform.
	ErrKeychainUnsupported = errors.New("keychain is not supported on this platform")

	// ErrKeychainItemNotFound is returned when the requested keychain item does not exist.
	ErrKeychainItemNotFound = errors.New("keychain item not found")

	// ErrKeychainAccessDenied is returned when access to the keychain is denied or locked.
	ErrKeychainAccessDenied = errors.New("keychain access denied or locked")
)

// ClaudeCredentialService is the macOS Keychain service name used by Claude Code.
const ClaudeCredentialService = "Claude Code-credentials"

// KeychainReader reads credentials from the system keychain.
type KeychainReader interface {
	// GetGenericPassword retrieves generic password bytes matching service and optional account.
	GetGenericPassword(ctx context.Context, service string, account string) ([]byte, error)
}

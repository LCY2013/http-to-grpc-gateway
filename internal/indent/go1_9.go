//go:build !go1.10
// +build !go1.10

package indent

func Indent() string {
	// In Go 1.9 and older, we need to add indentation
	// after newlines in the flag doc strings.
	return "    \t"
}

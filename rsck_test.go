package rsck

import (
	"fmt"
	"testing"
)

func TestParseForwardUri(t *testing.T) {
	fmt.Println(ParseForwardUri("udp://:2342>test://localhost:5342"))
}

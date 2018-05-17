package rsck

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/Centny/gwf/log"
)

var ShowLog int

func log_d(f string, args ...interface{}) {
	if ShowLog > 1 {
		log.D_(1, f, args...)
	}
}

func ParseForwardUri(uri string) (network, local, name, remote string, limit int, err error) {
	parts := strings.SplitN(uri, ">", 2)
	if len(parts) < 2 {
		err = fmt.Errorf("invalid forward uri(%v)", uri)
		return
	}
	lurl, err := url.Parse(parts[0])
	if err != nil {
		return
	}
	network = lurl.Scheme
	local = lurl.Host
	rurl, err := url.Parse(parts[1])
	if err != nil {
		return
	}
	name = rurl.Scheme
	remote = rurl.Host
	lv := rurl.Query().Get("limit")
	if len(lv) > 0 {
		limit, err = strconv.Atoi(lv)
	}
	return
}

package main

import (
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/Centny/gwf/pool"

	"github.com/Centny/gwf/netw"

	"github.com/Centny/gwf/routing"
	"github.com/Centny/gwf/util"

	"github.com/Centny/gwf/log"

	"github.com/sutils/rsck"
)

type ArrayFlags []string

func (a *ArrayFlags) String() string {
	return strings.Join(*a, ",")
}

func (a *ArrayFlags) Set(value string) error {
	*a = append(*a, value)
	return nil
}

var runServer = flag.Bool("s", false, "start as reverse channel server")
var listenAddr = flag.String("l", ":8241", "reverse/echo server listent address")
var webAddr = flag.String("w", ":8242", "web listent address")
var auth = flag.String("auth", "", "the basic auth(required)")
var acl ArrayFlags
var forword ArrayFlags
var aclFile = flag.String("aclf", "", "the file of reverse access control level(required if not acl)")
var forwardFile = flag.String("forward", "", "the file of the reverse forward")

var runRunner = flag.Bool("r", false, "start as reverse channel runner")
var name = flag.String("name", "", "the runner name")
var server = flag.String("server", "", "the server address")
var token = flag.String("token", "", "the login token")

var runEcho = flag.Bool("e", true, "start as echo server")

func init() {
	flag.Var(&acl, "acl", "the reverse access control level(required if not aclf)")
	flag.Var(&forword, "f", "the reverse forward")
}

// var acl=flag.StringVar(p *string, name string, value string, usage string)

func main() {
	flag.Parse()
	if *runServer {
		startServer()
	} else if *runRunner {
		startRunner()
	} else {
		startEcho()
	}
}

func startEcho() {
	log.D("listen echo server on %v", *listenAddr)
	echo, err := rsck.NewEchoServer("tcp", *listenAddr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
		return
	}
	echo.Start()
	make(chan int) <- 0
}

func startRunner() {
	if len(*server) < 1 || len(*name) < 1 || len(*token) < 1 {
		flag.Usage()
		os.Exit(1)
		return
	}
	// netw.ShowLog = true
	// netw.ShowLog_C = true
	// impl.ShowLog = true
	netw.MOD_MAX_SIZE = 4
	pool.SetBytePoolMax(1024 * 1024 * 4)
	runner := rsck.NewChannelRunner(*server, *name, *token)
	runner.Start()
	make(chan int) <- 0
}

var HTML = `
<html>

<head>
    <meta charset="UTF-8">
    <meta name="renderer" content="webkit|ie-comp|ie-stand">
    <meta http-equiv="X-UA-Compatible" content="IE=Edge,chrome=1" />
    <meta http-equiv="pragma" content="no-cache">
    <meta http-equiv="cache-control" content="no-cache">
    <style type="text/css">
        .list table {
            border-collapse: collapse;
            border: 1px solid black;
        }

        .list td {
            border: 1px solid black;
            padding: 8px;
            text-align: center;
        }

        .list th {
            border: 1px solid black;
            padding: 8px;
            text-align: center;
        }

        form table {
            border: 0px;
            padding: 0px;
            margin: 0px;
            border-spacing: 0px;
        }

        form td {
            border: 0px;
            padding: 0px;
            margin: 0px;
            border-spacing: 0px;
        }

        form th {
            border: 0px;
            padding: 0px;
            margin: 0px;
            border-spacing: 0px;
        }

        form textarea {
            resize: none;
            width: 240px;
            height: 50px;
            max-width: 300px;
            max-height: 50px;
            margin: 0px;
            padding: 0px;
        }
    </style>
</head>

<body>
    <form action="add" method="POST">
        <table>
            <td>
                <textarea name="forwards"></textarea>
            </td>
            <td>
                <input id="submit" type="submit" value="Submit" />
            </td>
        </table>
    </form>
    <p class="list">
        <table>
            <tr>
                <th>No</th>
                <th>Forward</th>
                <th>Action</th>
            </tr>
            {{range $k, $v := .forwards}}
            <tr>
                <td>{{$k}}</td>
                <td>{{$v}}</td>
                <td>
                    <a href="remove?idx={{$k}}">remove</a>
                </td>
            </tr>
            {{end}}
        </table>
    </p>
</body>

</html>
`

func startServer() {
	if len(*auth) < 1 && len(acl) < 1 && len(*aclFile) < 1 {
		flag.Usage()
		os.Exit(1)
		return
	}
	netw.MOD_MAX_SIZE = 4
	pool.SetBytePoolMax(1024 * 1024 * 4)
	server := rsck.NewChannelServer(*listenAddr, "Reverse Server")
	//
	forwards := map[string]bool{}
	forwardsLck := sync.RWMutex{}
	var sortedForwards = func() []string {
		fs := []string{}
		forwardsLck.RLock()
		for key := range forwards {
			fs = append(fs, key)
		}
		forwardsLck.RUnlock()
		sort.Sort(util.NewStringSorter(fs))
		return fs
	}
	//
	routing.HFilterFunc("^.*$", func(hs *routing.HTTPSession) routing.HResult {
		username, password, ok := hs.R.BasicAuth()
		if ok && *auth == fmt.Sprintf("%v:%s", username, password) {
			return routing.HRES_CONTINUE
		}
		hs.W.Header().Set("WWW-Authenticate", "Basic realm=Reverse Server")
		hs.W.WriteHeader(401)
		hs.Printf("%v", "401 Unauthorized")
		return routing.HRES_RETURN
	})
	routing.HFunc("^/remove(\\?.*)?$", func(hs *routing.HTTPSession) routing.HResult {
		var idx int
		var err = hs.ValidF(`idx,R|I,R:-1`, &idx)
		if err != nil {
			return hs.Printf("%v", err)
		}
		var fs = sortedForwards()
		var f = fs[idx]
		network, local, _, _, _, _ := rsck.ParseForwardUri(f)
		server.RemoveForward(network, local)
		forwardsLck.Lock()
		delete(forwards, f)
		forwardsLck.Unlock()
		hs.Redirect("/")
		return routing.HRES_RETURN
	})
	routing.HFunc("^/add(\\?.*)?$", func(hs *routing.HTTPSession) routing.HResult {
		for _, f := range strings.Split(hs.RVal("forwards"), "\n") {
			f = strings.TrimSpace(f)
			network, local, name, remote, limit, err := rsck.ParseForwardUri(f)
			if err != nil {
				return hs.Printf("%v", err)
			}
			err = server.AddForward(network, local, name, remote, limit)
			if err != nil {
				return hs.Printf("%v", err)
			}
			forwardsLck.Lock()
			forwards[f] = true
			forwardsLck.Unlock()
		}
		hs.Redirect("/")
		return routing.HRES_RETURN
	})
	tpl, _ := template.New("n").Parse(HTML)
	routing.HFunc("^.*$", func(hs *routing.HTTPSession) routing.HResult {
		tpl.Execute(hs.W, map[string]interface{}{
			"forwards": sortedForwards(),
		})
		return routing.HRES_RETURN
	})
	if len(*aclFile) > 0 {
		bys, err := ioutil.ReadFile(*aclFile)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		acl = append(acl, strings.Split(string(bys), "\n")...)
	}
	for _, entry := range acl {
		entry = strings.TrimSpace(entry)
		if len(entry) < 1 {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) < 2 {
			log.W("the acl entry(%v) is invalid", entry)
			continue
		}
		server.ACL[parts[0]] = parts[1]
	}
	if len(*forwardFile) > 0 {
		bys, err := ioutil.ReadFile(*forwardFile)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		forword = append(forword, strings.Split(string(bys), "\n")...)
	}
	for _, f := range forword {
		f = strings.TrimSpace(f)
		if len(f) < 1 {
			continue
		}
		network, local, name, remote, limit, err := rsck.ParseForwardUri(f)
		if err != nil {
			log.W("the forward entry(%v) is invalid", f)
			continue
		}
		err = server.AddForward(network, local, name, remote, limit)
		if err != nil {
			log.W("add forward by entry(%v) fail with %v", f, err)
			continue
		}
		forwardsLck.Lock()
		forwards[f] = true
		forwardsLck.Unlock()
	}
	server.Start()
	log.D("listen web server on %v", *webAddr)
	fmt.Println(routing.ListenAndServe(*webAddr))
}

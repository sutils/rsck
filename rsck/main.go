package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"os/user"
	"sort"
	"strings"

	"github.com/Centny/gwf/pool"
	"github.com/Centny/gwf/util"

	"github.com/Centny/gwf/netw"

	"github.com/Centny/gwf/routing"

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
var workspace *string

var runRunner = flag.Bool("r", false, "start as reverse channel runner")
var name = flag.String("name", "", "the runner name")
var server = flag.String("server", "", "the server address")
var token = flag.String("token", "", "the login token")

var runEcho = flag.Bool("e", true, "start as echo server")

func init() {
	flag.Var(&acl, "acl", "the reverse access control level(required if not aclf)")
	flag.Var(&forword, "f", "the reverse forward")
	usr, err := user.Current()
	if err != nil {
		panic(err)
	}
	workspace = flag.String("ws", usr.HomeDir+"/.rsck", "the workspace")
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
	runner.AddDailer(rsck.NewCmdDailer())
	runner.AddDailer(rsck.NewWebDailer())
	runner.AddDailer(rsck.NewTCPDailer())
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
        .boder_1px_t {
            border-collapse: collapse;
            border: 1px solid black;
        }

        .boder_1px {
            border: 1px solid black;
            padding: 8px;
            text-align: center;
        }

		.noneborder {
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
		<table class="boder_1px_t" >
			<tr class="boder_1px" >
				<th class="boder_1px" >No</th>
				<th class="boder_1px" >Name</th>
				<th class="boder_1px" >Online</th>
				<th class="boder_1px" >Remote</th>
				<th class="boder_1px" >Forward</th>
			</tr>
			{{range $k, $v := .ns}}
			{{$channel := index $.forwards $v}}
			<tr class="boder_1px" >
				<td class="boder_1px" >{{$k}}</td>
				<td class="boder_1px" >{{$channel.Name}}</td>
				<td class="boder_1px" >{{$channel.Online}}</td>
				<td class="boder_1px" >{{$channel.Remote}}</td>
				<td class="boder_1px" >
					<table class="noneborder" >
						{{range $i, $f := $channel.FS}}
						<tr class="noneborder" style="height:20px">
							<td class="noneborder" >{{$f}}</td>
							<td class="noneborder" >
								<a style="margin-left:10px;" href="remove?local={{$f.Local}}">Remove</a>
							</td>
						</tr>
						{{end}}
					</table>
				</td>
			</tr>
			{{end}}
		</table>
	</p>
	<table class="boder_1px" style="position:absolute;right:30px;top:5px;">
		{{range $i, $r := $.recents}}
		{{$f := index $r "forward"}}
		<tr class="noneborder" style="height:20px;text-align:left;">
			<td class="noneborder" >{{$f}}</td>
			<td class="noneborder" >
				<a style="margin-left:10px;" href="add?forwards={{$f}}">Add</a>
			</td>
		</tr>
		{{end}}
	</table>
</body>
</html>
`

func readRecent() (recent map[string]int) {
	recent = map[string]int{}
	bys, err := ioutil.ReadFile(*workspace + "/recent.json")
	if err != nil && !os.IsNotExist(err) {
		log.W("read recent from %v fail with %v", *workspace+"/recent.json", err)
		return
	}
	err = json.Unmarshal(bys, &recent)
	if err != nil {
		log.W("nmarshal recent json data on %v fail with %v", *workspace+"/recent.json", err)
		return
	}
	return
}

func writeRecent(recent map[string]int) {
	bys, err := json.Marshal(recent)
	if err != nil {
		log.W("marshal recent json fail with %v", err)
		return
	}
	err = ioutil.WriteFile(*workspace+"/recent.json", bys, os.ModePerm)
	if err != nil {
		log.W("save recent to %v fail with %v", *workspace+"/recent.json", err)
		return
	}
}

func startServer() {
	if len(*auth) < 1 && len(acl) < 1 && len(*aclFile) < 1 {
		flag.Usage()
		os.Exit(1)
		return
	}
	os.MkdirAll(*workspace, os.ModePerm)
	netw.MOD_MAX_SIZE = 4
	pool.SetBytePoolMax(1024 * 1024 * 4)
	server := rsck.NewChannelServer(*listenAddr, "Reverse Server")
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
		var local string
		var err = hs.ValidF(`
			local,R|S,L:0;
			`, &local)
		if err != nil {
			return hs.Printf("%v", err)
		}
		server.RemoveForward(local)
		hs.Redirect("/")
		return routing.HRES_RETURN
	})
	routing.HFunc("^/add(\\?.*)?$", func(hs *routing.HTTPSession) routing.HResult {
		oldRecent := readRecent()
		for _, f := range strings.Split(hs.RVal("forwards"), "\n") {
			f = strings.TrimSpace(f)
			if len(f) < 1 {
				continue
			}
			err := server.AddUriForward(f)
			if err != nil {
				return hs.Printf("%v", err)
			}
			oldRecent[f]++
		}
		writeRecent(oldRecent)
		hs.Redirect("/")
		return routing.HRES_RETURN
	})
	tpl, _ := template.New("n").Parse(HTML)
	routing.HFunc("^.*$", func(hs *routing.HTTPSession) routing.HResult {
		ns, forwards := server.AllForwards()
		oldRecent := readRecent()
		recents := []util.Map{}
		for f, c := range oldRecent {
			oldForward, err := rsck.NewForward(f)
			if err != nil {
				continue
			}
			using := false
			if channel, ok := forwards[oldForward.Name]; ok {
				for _, runningForward := range channel.FS {
					if oldForward.String() == runningForward.String() {
						using = true
						break
					}
				}
			}
			if using {
				continue
			}
			recents = append(recents, util.Map{
				"forward": oldForward,
				"used":    c,
			})
		}
		sorter := util.NewMapIntSorter("used", recents)
		sorter.Desc = true
		sort.Sort(sorter)
		err := tpl.Execute(hs.W, map[string]interface{}{
			"ns":       ns,
			"forwards": forwards,
			"recents":  recents,
		})
		if err != nil {
			log.E("Parse html fail with %v", err)
		}
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
		err := server.AddUriForward(f)
		if err != nil {
			log.W("add forward by entry(%v) fail with %v", f, err)
			continue
		}
	}
	server.Start()
	log.D("listen web server on %v", *webAddr)
	fmt.Println(routing.ListenAndServe(*webAddr))
}

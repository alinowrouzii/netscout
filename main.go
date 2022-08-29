package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultPort  = "4000"
	defaultRoute = "/mterics"
)

type App struct {
	// data mutex
	m sync.Mutex
	// next to access mutex
	n sync.Mutex
	// low priority mutexes
	l sync.Mutex

	hosts map[string][]struct {
		address string
		port    string
	}
}

func connectionStatus(host string, port string) bool {
	timeout := 500 * time.Millisecond
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	if conn != nil {
		defer conn.Close()
		return true
	}
	return false
}

func (app *App) checkStatus() {

	// forloop over all hosts and create goroutine for each host address
	for appName := range app.hosts {
		app := app.hosts[appName]
		for appHostIndex := range app {
			appHost := app[appHostIndex]
			// fmt.Println(appHost)
			address := appHost.address
			port := appHost.port

			go func(address string, port string) {
				ticker := time.NewTicker(5 * time.Second)
				quit := make(chan struct{})

				for {
					select {
					case <-ticker.C:
						st := connectionStatus(address, port)
						if st {
							log.Println("Succeed!", address, port)
						} else {
							log.Println("Failed!", address, port)
						}
					case <-quit:
						ticker.Stop()
						return
					}

				}

			}(address, port)
		}

	}

}

func (metric *App) metricsHandler(w http.ResponseWriter, r *http.Request) {

	// here we inform our goroutine calculator to does not enter to our critical section if
	// handler wants to show the result to the client
	metric.n.Lock()
	metric.m.Lock()
	metric.n.Unlock()
	// lines := ""
	// lines += fmt.Sprintf("# HELP {name_space}_log_exporter_requests_total Number of request with specified status and method.\n")
	// lines += fmt.Sprintf("# TYPE {name_space}_log_exporter_requests_total counter\n")
	// for appName := range metric.methods {
	// 	for method := range metric.methods[appName] {
	// 		for status := range metric.methods[appName][method] {
	// 			lines += fmt.Sprintf("%s_log_exporter_requests_total{method=\"%s\", status=\"%s\"} %d\n", appName, method, status, metric.methods[appName][method][status])
	// 		}
	// 	}
	// }
	// w.Write([]byte(lines))
	metric.m.Unlock()
}

var ymlPath string

func init() {
	flag.StringVar(&ymlPath, "config", "", "specify the path of yaml config")
	flag.Parse()
}

func main() {
	if ymlPath == "" {
		panic("config path is required. Type --help for more info")
	}
	hosts, _, _ := parseYml(ymlPath)

	// mux := tinymux.NewTinyMux()

	app := &App{
		hosts: hosts,
	}
	app.checkStatus()

	ch := make(chan string)
	<-ch

	// fmt.Println(route, listenPort)
	// mux.GET(route, http.HandlerFunc(metric.metricsHandler))

	// http.ListenAndServe(fmt.Sprintf(":%s", listenPort), mux)

}

func splitQoutes(s string) []string {
	insideQoute := false
	out := strings.FieldsFunc(s, func(r rune) bool {
		if r == '"' {
			insideQoute = !insideQoute
		}
		return r == '"' || (!insideQoute && r == ' ')
	})
	return out
}

func parseYml(ymlPath string) (map[string][]struct {
	address string
	port    string
}, string, string) {
	parsedYml := make(map[interface{}]interface{})
	data, err := ioutil.ReadFile(ymlPath)
	fmt.Println(parsedYml)

	if err != nil {
		panic(err)
	}
	err = yaml.Unmarshal(data, &parsedYml)
	if err != nil {
		panic(err)
	}

	fmt.Println(parsedYml["apps"])

	apps, ok := parsedYml["apps"]

	if !ok {
		panic("invalid yml format!")
	}

	main, ok := parsedYml["main"]
	parsedListen := defaultPort
	parsedRoute := defaultRoute
	if ok {
		parsedMain, ok := main.(map[string]interface{})
		if !ok {
			panic("invalid yaml format")
		}

		listen, ok := parsedMain["listen"]
		if ok {
			parsedListen, ok = listen.(string)
			if !ok {
				panic("invalid yaml format")
			}
		}

		route, ok := parsedMain["route"]
		if ok {
			parsedRoute, ok = route.(string)
			if !ok {
				panic("invalid yaml format")
			}
		}
	}

	hostsResult := make(map[string][]struct {
		address string
		port    string
	})

	for appName := range apps.(map[string]interface{}) {
		app := apps.(map[string]interface{})[appName].(map[string]interface{})
		hosts, ok := app["hosts"]
		fmt.Println(hosts)
		if !ok {
			panic("invalid yaml format")
		}
		parsedHosts := make([]string, 0)
		switch t := hosts.(type) {
		case []interface{}:
			for index := range t {
				// castedValue, ok := value.(string)
				host, ok := t[index].(map[string]interface{})
				if !ok {
					panic("invalid yaml format")
				}

				castedAddress, ok := host["address"].(string)

				if !ok {
					panic("invalid yaml format")
				}
				castedPort, ok := host["port"].(string)
				if !ok {
					panic("invalid yaml format")
				}
				hostSt := struct {
					address string
					port    string
				}{
					address: castedAddress,
					port:    castedPort,
				}
				hostsResult[appName] = append(hostsResult[appName], hostSt)
			}
		default:
			panic("invalid yaml format")
		}

		fmt.Println("parsedLogs: ", parsedHosts)

	}

	fmt.Println(parsedListen)
	fmt.Println(parsedRoute)
	fmt.Println(hostsResult)
	return hostsResult, parsedListen, parsedRoute
}

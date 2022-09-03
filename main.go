package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	// "github.com/go-redis/redis/v8"
	redisTimeSeries "github.com/alinowrouzii/network-status-checker/modules/redis"
	tinymux "github.com/alinowrouzii/tiny-mux"

	"gopkg.in/yaml.v3"
)

const (
	defaultPort      = "4000"
	defaultRedisAddr = ":6379"
)

type App struct {
	l sync.Mutex

	hosts map[string][]struct {
		address string
		port    string
	}
	redisConn *redisTimeSeries.RedisConn
}

type queryBody struct {
	Range struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"range"`
	IntervalMs int64 `json:"intervalMs"`
	Targets    []struct {
		Target string `json:"target"`
		Type   string `json:"type"`
	} `json:"targets"`
}

type timeSeriResponse struct {
	Target     string          `json:"target"`
	Datapoints [][]interface{} `json:"datapoints"`
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
		hosts := app.hosts[appName]

		for appHostIndex := range hosts {
			appHost := hosts[appHostIndex]
			address := appHost.address
			port := appHost.port

			addressPortPair := fmt.Sprintf("%s:%s", address, port)
			app.redisConn.AddKeyToRedis(addressPortPair)

			go func(addressPortPair string, redisConn *redisTimeSeries.RedisConn) {
				ticker := time.NewTicker(1 * time.Second)
				quit := make(chan struct{})

				for {
					select {
					case <-ticker.C:
						st := connectionStatus(address, port)
						if st {

							log.Println("shit in if", addressPortPair)
							app.l.Lock()
							err := redisConn.SetInRedis(addressPortPair, 20)
							time.Sleep(time.Millisecond)
							app.l.Unlock()
							if err != nil {
								fmt.Println("err in goroutine", err)
							}
						} else {
							log.Println("shit in else", addressPortPair)
							app.l.Lock()
							err := redisConn.SetInRedis(addressPortPair, 10)
							time.Sleep(time.Millisecond)
							app.l.Unlock()
							if err != nil {
								fmt.Println("err in goroutine", err)
							}
						}
					case <-quit:
						ticker.Stop()
						return
					}
				}
			}(addressPortPair, app.redisConn)
		}
	}
}

var ymlPath string

func init() {
	flag.StringVar(&ymlPath, "config", "", "specify the path of yaml config")
	flag.Parse()
}

func (app *App) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (app *App) searchHandler(w http.ResponseWriter, r *http.Request) {
	keys := app.redisConn.SearchOverKeys()
	json.NewEncoder(w).Encode(keys)
}

func (app *App) annotationHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (app *App) queryHandler(w http.ResponseWriter, r *http.Request) {

	var q queryBody
	// fmt.Println(r.Body, "here is body")
	err := json.NewDecoder(r.Body).Decode(&q)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request!"))
	}

	// res chan with bufferSize=10
	fmt.Println("here is time", q.Range.From, q.Range.To, q.IntervalMs)
	resChan := make(chan [2]interface{}, 10)
	fromTime, err := time.Parse(time.RFC3339, q.Range.From)
	toTime, err := time.Parse(time.RFC3339, q.Range.To)
	for _, target := range q.Targets {
		targetName := target.Target
		go app.redisConn.GetTimeSeriesfunc(resChan, targetName, fromTime.UnixMilli(), toTime.UnixMilli(), q.IntervalMs)
	}
	result := make([]timeSeriResponse, 0)
	for _ = range q.Targets {
		res := <-resChan
		target, ok := res[0].(string)
		if !ok {
			log.Println("NotOK2")
			continue
		}
		datapoints, ok := res[1].([][]interface{})
		if !ok {
			log.Println("NotOK")
			continue
		}
		timeSeriResponse := timeSeriResponse{
			Target:     target,
			Datapoints: datapoints,
		}
		result = append(result, timeSeriResponse)
	}
	json.NewEncoder(w).Encode(result)
}

func corsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// log.Println("hellllow rodl")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding")
		h.ServeHTTP(w, r)
	})

}

func optionsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		}
		fmt.Println(r.URL, r.Method, "inside op")
		h.ServeHTTP(w, r)
	})
}

var ctx = context.Background()

func main() {

	if ymlPath == "" {
		panic("config path is required. Type --help for more info")
	}
	hosts, listenPort, redisAddr := parseYml(ymlPath)
	redisConn := redisTimeSeries.RedisInit(redisAddr)

	mux := tinymux.NewTinyMux()
	app := &App{
		hosts:     hosts,
		redisConn: redisConn,
	}

	app.checkStatus()

	mux.Use(corsMiddleware)
	mux.Use(optionsMiddleware)
	mux.POST("/search", http.HandlerFunc(app.searchHandler))
	mux.POST("/query", http.HandlerFunc(app.queryHandler))
	mux.GET("/annotation", http.HandlerFunc(app.annotationHandler))
	mux.GET("/", http.HandlerFunc(app.healthHandler))

	http.ListenAndServe(fmt.Sprintf(":%s", listenPort), mux)
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
	parsedRedisAddr := defaultRedisAddr

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

		redisAddr, ok := parsedMain["redisAddr"]
		if ok {
			parsedRedisAddr, ok = redisAddr.(string)
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
	fmt.Println(hostsResult)
	return hostsResult, parsedListen, parsedRedisAddr
}

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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	// "github.com/go-redis/redis/v8"
	tinymux "github.com/alinowrouzii/tiny-mux"
	"github.com/gomodule/redigo/redis"

	"gopkg.in/yaml.v3"
)

const (
	defaultPort  = "4000"
	defaultRoute = "/mterics"
	// retention time is one day (ms)
	redisRetentionTime = 86400000
	// redisRetentionTime = 10000
)

type redisConn struct {
	pool *redis.Pool
}
type App struct {
	l sync.Mutex

	hosts map[string][]struct {
		address string
		port    string
	}
	redisConn *redisConn
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
			// fmt.Println(appHost)
			address := appHost.address
			port := appHost.port

			addressPortPair := fmt.Sprintf("%s:%s", address, port)
			app.redisConn.addKeyToRedis(addressPortPair)

			go func(addressPortPair string, redisConn *redisConn) {
				ticker := time.NewTicker(1 * time.Second)
				quit := make(chan struct{})

				for {
					select {
					case <-ticker.C:
						st := connectionStatus(address, port)
						if st {

							log.Println("shit in if", addressPortPair)
							app.l.Lock()
							err := redisConn.setInRedis(addressPortPair, 20)
							time.Sleep(time.Millisecond)
							app.l.Unlock()
							if err != nil {
								fmt.Println("err in goroutine", err)
							}
						} else {
							log.Println("shit in else", addressPortPair)
							app.l.Lock()
							err := redisConn.setInRedis(addressPortPair, 10)
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

func (metric *App) metricsHandler(w http.ResponseWriter, r *http.Request) {

	// here we inform our goroutine calculator to does not enter to our critical section if
	// handler wants to show the result to the client
}

var ymlPath string

func init() {
	flag.StringVar(&ymlPath, "config", "", "specify the path of yaml config")
	flag.Parse()
}

func newPool(server string) *redis.Pool {

	return &redis.Pool{

		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,

		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", server)
			if err != nil {
				return nil, err
			}
			return c, err
		},

		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}
}

func redisInit(redisAddr string) *redisConn {
	pool := newPool(redisAddr)

	return &redisConn{
		pool: pool,
	}
}

func (conn *redisConn) addKeyToRedis(keyname string) error {
	redisConn := conn.pool.Get()
	defer redisConn.Close()

	// TODO: change alter to create after test
	ts, err := redisConn.Do("TS.CREATE", keyname, "RETENTION", redisRetentionTime)

	log.Println("addKey", ts, err, reflect.TypeOf(err))

	if err != nil {
		switch e := err.(type) {
		case redis.Error:
			if !strings.Contains(e.Error(), "key already exists") {
				log.Fatal("shit! ", e.Error())
			}
		default:
			log.Fatal("shit", err)
		}
	}

	return err
}
func (conn *redisConn) setInRedis(keyname string, value float64) error {
	redisConn := conn.pool.Get()
	defer redisConn.Close()

	ts, err := redisConn.Do("TS.ADD", keyname, "*", value)

	log.Println("timestamp", ts)
	return err
}

func (conn *redisConn) getFromRedis(keyName string, from int64, to int64) error {
	redisConn := conn.pool.Get()
	defer redisConn.Close()

	ts, err := redisConn.Do("TS.RANGE", keyName, from, to)
	log.Println("============")
	log.Println("timestamp", ts)
	return err
}

func (conn *redisConn) scanKeys(keyChann chan string, cursor int) {
	redisConn := conn.pool.Get()
	// n := int64(10)
	defer redisConn.Close()

	log.Println("entering in the scanKeys", cursor)
	count := 1
	res, err := redisConn.Do("SCAN", cursor, "COUNT", count)
	if err != nil {
		log.Fatal("err", err)
	}

	resInterface, ok := res.([]interface{})
	if !ok {
		log.Fatal("shit!")
	}

	// log.Println(reflect.TypeOf(resInterface[0]))
	remaingKeysInterface, ok := resInterface[0].([]uint8)
	if !ok {
		log.Fatal("shit!!!!")
	}
	newCursor, err := strconv.Atoi(string(remaingKeysInterface))
	if err != nil {
		log.Fatal("shit!", err)
	}

	// log.Println(newCursor, "hehe")

	keysInterface, ok := resInterface[1].([]interface{})
	if !ok {
		log.Fatal("shit")
	}

	for _, el := range keysInterface {
		keyBytes, ok := el.([]uint8)
		if !ok {
			log.Fatal("shit")
		}
		key := string(keyBytes)
		// log.Println("writing to channel", key)
		// log.Println("writing to channel", key)
		keyChann <- key
	}
	// log.Println("here is keys: ", arr)

	if newCursor > 0 {
		// call scanKeys recursively for remaing keys
		conn.scanKeys(keyChann, newCursor)
	} else {
		// send goodbye to the channel. In this way other goroutine will be inform
		// that all keys read and can close this channel
		keyChann <- "goodBye"
	}
}

func (conn *redisConn) addKeys(keyChann chan string, goodByeChann chan []string) {
	redisConn := conn.pool.Get()
	defer redisConn.Close()
	keys := make([]string, 0)
	for {
		key := <-keyChann

		if key == "goodBye" {
			close(keyChann)
			break
		}

		info, err := redisConn.Do("type", key)

		if err != nil {
			log.Println("shit!!!!!", err)
			break
		}

		if info == "TSDB-TYPE" {
			keys = append(keys, key)
		}

		log.Println("received key: ", key, info)
	}
	log.Println(keys)

	goodByeChann <- keys
}

func (conn *redisConn) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (conn *redisConn) searchHandler(w http.ResponseWriter, r *http.Request) {

	// channel with bufferSize 100
	keyChann := make(chan string, 100)
	goodByeChann := make(chan []string)

	go conn.scanKeys(keyChann, 0)

	go conn.addKeys(keyChann, goodByeChann)

	keys := <-goodByeChann
	close(goodByeChann)

	json.NewEncoder(w).Encode(keys)
}

func corsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// w.Header().Set("Access-Control-Allow-Origin", "localhost:3000")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
		h.ServeHTTP(w, r)
	})

}

func optionsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("hellllllo")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
			return
		}
	})

}

var ctx = context.Background()

func main() {

	if ymlPath == "" {
		panic("config path is required. Type --help for more info")
	}
	hosts, listenPort, _ := parseYml(ymlPath)

	redisConn := redisInit(":6379")

	mux := tinymux.NewTinyMux()

	_ = &App{
		hosts:     hosts,
		redisConn: redisConn,
	}
	// app.checkStatus()

	// for {
	// 	app.redisConn.getFromRedis("localhost:80", 1661947359361, 1661976404011)
	// 	time.Sleep(time.Second)
	// }

	// ch := make(chan string)
	// <-ch

	// fmt.Println(route, listenPort)
	mux.Use(corsMiddleware)
	mux.GET("/search", http.HandlerFunc(redisConn.searchHandler))
	mux.GET("/", http.HandlerFunc(redisConn.healthHandler))

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

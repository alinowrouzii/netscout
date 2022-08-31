package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	// "github.com/go-redis/redis/v8"
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
							err := redisConn.setInRedis(addressPortPair, 2)
							time.Sleep(time.Millisecond)
							app.l.Unlock()
							if err != nil {
								fmt.Println("err in goroutine", err)
							}
						} else {
							log.Println("shit in else", addressPortPair)
							app.l.Lock()
							err := redisConn.setInRedis(addressPortPair, 1)
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
	ts, err := redisConn.Do("TS.ALTER", keyname, "RETENTION", redisRetentionTime)

	log.Println("addKey", ts, err, reflect.TypeOf(err))

	if err != nil {
		switch e := err.(type) {
		case redis.Error:
			if !strings.Contains(e.Error(), "key already exists") {
				log.Fatal("shit! ", e.Error())
			} else {
				log.Fatal("shit", err)
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

var ctx = context.Background()

func main() {

	if ymlPath == "" {
		panic("config path is required. Type --help for more info")
	}
	hosts, _, _ := parseYml(ymlPath)

	redisConn := redisInit(":6379")

	// mux := tinymux.NewTinyMux()

	app := &App{
		hosts:     hosts,
		redisConn: redisConn,
	}
	app.checkStatus()

	// for {
	// 	app.redisConn.getFromRedis("localhost:80", 1661947359361, 1661976404011)
	// 	time.Sleep(time.Second)
	// }

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

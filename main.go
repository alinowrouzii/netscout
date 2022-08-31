package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
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
)

type redisConn struct {
	pool *redis.Pool
}
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

			go func(address string, port string, redisConn *redisConn) {
				addressPortPair := fmt.Sprintf("%s:%s", address, port)
				ticker := time.NewTicker(1 * time.Second)
				quit := make(chan struct{})

				for {
					select {
					case <-ticker.C:
						st := connectionStatus(address, port)
						if st {
							// redisConn.setInRedis()
							// line := fmt.Sprintf("<%s> %s:%s:Succeed", time.Now().String(), address, port)
							// fmt.Println(line)
							log.Println("shittttttttttt in if", addressPortPair)
							app.l.Lock()
							// err := redisConn.setInRedis("myapp", 2, addressPortPair)
							time.Sleep(time.Millisecond)
							app.l.Unlock()
							// if err != nil {
							// 	fmt.Println("err in goroutine", err)
							// }
						} else {
							// line := fmt.Sprintf("<%s> %s:%s:Failed", time.Now().String(), address, port)
							// fmt.Println(line)
							log.Println("shittttttttttt in else", addressPortPair)
							// time.Sleep(waitingTime)
							app.l.Lock()
							// err := redisConn.setInRedis("myapp", 1, addressPortPair)
							time.Sleep(time.Millisecond)
							app.l.Unlock()
							// if err != nil {
							// 	fmt.Println("err in goroutine", err)
							// }
						}

					case <-quit:
						ticker.Stop()
						return
					}

				}

			}(address, port, app.redisConn)
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

func redisInit() *redisConn {
	// client := redis.DialDatabase(0)

	redisHost := os.Getenv("REDIS_HOST")
	if redisHost == "" {
		redisHost = ":6379"
	}
	pool := newPool(redisHost)

	// fmt.Println(pool)

	// conn := pool.Get()

	// shit := map[string]string{
	// 	"localhost:4040": "1",
	// 	"localhost:4041": "1",
	// 	"localhost:80":   "1",
	// 	"localhost:4042": "1",
	// }

	// query := "TS.CTREATE shit LABELS "
	// argsArr := make([]string, 0)
	// for k, v := range shit {
	// 	argsArr = append(argsArr, k, v)
	// }
	// fmt.Println("here is your query", query)

	// fmt.Println(reply)

	// fmt.Println("=============================================================")
	// reply, err = conn.Do("TS.ALTER", "shit2", "LABELS", "localhost:4042", "2")
	// // reply, err := conn.Do("TS.ALTER", "shit", "LABELS", argsArr)

	// if err != nil {
	// 	log.Println("shith! ", err)
	// }

	// fmt.Println(reply)

	// fmt.Println("=============================================================")
	// reply, err = redis.Int64(conn.Do("TS.ADD", "shit", "*", fmt.Sprintf("%d", 1)))
	// if err != nil {
	// 	log.Fatal("shit!! ", err)
	// }
	// fmt.Println(reply)

	// reply, err = redis.Int64(conn.Do("TS.ADD", "shit2", "*", fmt.Sprintf("%d", 2), "LABELS", "localhost:4042", "2"))
	// if err != nil {
	// 	log.Fatal("shit2!! ", err)
	// }

	// fmt.Println(reply)

	// // TS.MRANGE - + FILTER area_id=32

	// reply, err = conn.Do("TS.MRANGE", "-", "+", "FILTER", "localhost:4042=1")
	// if err != nil {
	// 	log.Fatal("shit23!! ", err)
	// }

	// fmt.Println(reply)

	return &redisConn{
		pool: pool,
	}
}

func (conn *redisConn) addKeyToRedis(keyname string) error {
	redisConn := conn.pool.Get()
	defer redisConn.Close()

	ts, err := redisConn.Do("TS.CREATE", keyname)

	log.Println("addKey", ts, err)
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

	// ts, err := redisConn.Do("TS.RANGE", keyName, "-", "+")
	// ts, err := redisConn.Do("TS.RANGE", keyName, "-", "+", "FILTER_BY_TS", from, to)
	ts, err := redisConn.Do("TS.RANGE", keyName, from, to)
	log.Println("============")
	log.Println("timestamp", ts)
	return err
}

// func (conn *redisConn) getFromRedis(key string) (string, error) {
// 	val, err := conn.client.Get(ctx, key).Result()
// 	return val, err
// }

var ctx = context.Background()

func main() {

	if ymlPath == "" {
		panic("config path is required. Type --help for more info")
	}
	hosts, _, _ := parseYml(ymlPath)

	redisConn := redisInit()

	// redisConn.setInRedis("shit")
	// val, err := redisConn.getFromRedis("shit")
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Println("key", val)

	// ***************************Just for test****************************
	// Connect to localhost with no password
	// var client = redistimeseries.NewClient("localhost:6379", "nohelp", nil)
	// // * key should be in combination of appName and hostAddress. eg "AppOne:localhost:4040"
	// var keyname = "mytest"
	// _, haveit := client.Info(keyname)
	// if haveit != nil {
	// 	client.CreateKeyWithOptions(keyname, redistimeseries.CreateOptions{
	// 		Uncompressed:   false,
	// 		RetentionMSecs: 86400000,
	// 		Labels: map[string]string{
	// 			"localhost:4040": "1",
	// 			"localhost:4041": "1",
	// 			"localhost:80":   "1",
	// 		},
	// 	})
	// 	client.CreateKeyWithOptions(keyname+"_avg", redistimeseries.DefaultCreateOptions)
	// 	client.CreateRule(keyname, redistimeseries.AvgAggregation, 60, keyname+"_avg")
	// }
	// // Add sample with timestamp from server time and value 100
	// // TS.ADD mytest * 100
	// ts, err := client.AddAutoTs(keyname, 99)
	// if err != nil {
	// 	log.Fatal("Error:", err)
	// }
	// log.Println(ts)

	// res, err := client.Range(keyname, 1661800611332, 1661800981332)
	// if err != nil {
	// 	log.Fatal("Error:", err)
	// }
	// log.Println("here is result", res)
	// return
	// ********************************************************************

	// mux := tinymux.NewTinyMux()

	app := &App{
		hosts:     hosts,
		redisConn: redisConn,
	}
	// app.checkStatus()

	app.redisConn.addKeyToRedis("shit")
	app.redisConn.setInRedis("shit", 1)
	// app.redisConn.setInRedis("shit", 1)
	// app.redisConn.setInRedis("shit", 2)
	time.Sleep(time.Second)
	// app.redisConn.setInRedis("shit", 2)
	// time.Sleep(time.Second)
	// app.redisConn.setInRedis("shit", 1)
	// time.Sleep(time.Second)
	app.redisConn.getFromRedis("shit", 1661947359361, 1661976404011)

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

package redisTimeSeries

import (
	"errors"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
)

const (
	redisRetentionTime = 86400000
)

type RedisConn struct {
	pool *redis.Pool
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

func RedisInit(redisAddr string) *RedisConn {
	pool := newPool(redisAddr)

	return &RedisConn{
		pool: pool,
	}
}

func (conn *RedisConn) AddKeyToRedis(keyname string) error {
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
func (conn *RedisConn) SetInRedis(keyname string, value float64) error {
	redisConn := conn.pool.Get()
	defer redisConn.Close()

	ts, err := redisConn.Do("TS.ADD", keyname, "*", value)

	log.Println("timestamp", ts)
	return err
}

func (conn *RedisConn) GetFromRedis(keyName string, from int64, to int64, bucketSize float64) ([][]interface{}, *string, error) {
	redisConn := conn.pool.Get()
	defer redisConn.Close()
	fmt.Println("from to", from, to, bucketSize, keyName)

	dataPoints, err := redisConn.Do("TS.RANGE", keyName, from, to, "AGGREGATION", "avg", bucketSize)

	if err != nil {
		return nil, nil, err
	}

	dataPointsInterface, ok := dataPoints.([]interface{})
	if !ok {
		return nil, nil, errors.New("there is a fucking error when ranging over timeseries")
	}
	dataPointsRes := make([][]interface{}, 0)
	for _, ts := range dataPointsInterface {
		tsInterface, ok := ts.([]interface{})
		if !ok {
			return nil, nil, errors.New("there is a fucking error when ranging over timeseries")
		}
		// here we need to reverse the timeserie and value
		tsInterface = []interface{}{tsInterface[1], tsInterface[0]}
		dataPointsRes = append(dataPointsRes, tsInterface)

	}
	log.Println("============")
	log.Println("timestamp", dataPointsInterface)
	return dataPointsRes, &keyName, err
}

func (conn *RedisConn) GetTimeSeriesfunc(resChann chan [2]interface{}, targetName string, from int64, to int64, interval int64) {
	dataPointsRes, target, err := conn.GetFromRedis(targetName, from, to, float64(interval))

	if err == nil {
		res := [2]interface{}{
			*target,
			dataPointsRes,
		}
		resChann <- res
	} else {
		log.Println("fuck", err)
	}
}

func (conn *RedisConn) scanKeys(keyChann chan string, cursor int) {
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

func (conn *RedisConn) addKeys(keyChann chan string, goodByeChann chan []string) {
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

// finds keys with TS-DB type
func (conn *RedisConn) SearchOverKeys() []string {

	// channel with bufferSize 100
	keyChann := make(chan string, 100)
	goodByeChann := make(chan []string)

	go conn.scanKeys(keyChann, 0)
	go conn.addKeys(keyChann, goodByeChann)

	keys := <-goodByeChann
	close(goodByeChann)

	return keys
}

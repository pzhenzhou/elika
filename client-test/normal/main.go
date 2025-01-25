package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/pzhenzhou/elika/client-test/testutils"
	"time"

	"github.com/redis/go-redis/v9"
)

func multiT(ctx context.Context, client *redis.Client) {
	keyFoo := testutils.GenerateKey("multiT_foo")
	keyBar := testutils.GenerateKey("multiT_bar")
	pipe := client.TxPipeline()
	incrFoo := pipe.Incr(ctx, keyFoo)
	incrBar := pipe.Incr(ctx, keyBar)
	_, err := pipe.Exec(ctx)
	if err != nil {
		testutils.Logger.Info("Error executing pipeline", "Error", err)
		panic(err)
	}
	if incrFoo.Val() != 1 || incrBar.Val() != 1 {
		testutils.Logger.Info("Error executing pipeline", "incrFoo", incrFoo.Val(), "incrBar", incrBar.Val())
		panic(fmt.Sprintf("Expected 1, got foo: %d, bar: %d", incrFoo.Val(), incrBar.Val()))
	}
}

func listT(ctx context.Context, client *redis.Client) {
	key := testutils.GenerateKey("listT")
	err := client.LPush(ctx, key, "world").Err()
	if err != nil {
		panic(err)
	}
	err = client.LPush(ctx, key, "hello").Err()
	if err != nil {
		panic(err)
	}
	lrange, err := client.LRange(ctx, key, 0, -1).Result()
	if err != nil {
		panic(err)
	}
	expectedLRange := []string{"hello", "world"}
	if len(lrange) != len(expectedLRange) {
		panic("LRANGE result length mismatch")
	}
	for i, v := range lrange {
		if v != expectedLRange[i] {
			panic(fmt.Sprintf("Expected %s, got %s", expectedLRange[i], v))
		}
	}
}

func zaddT(ctx context.Context, client *redis.Client) {
	key := testutils.GenerateKey("zaddT")
	err := client.ZAdd(ctx, key, redis.Z{Score: 1, Member: "one"}, redis.Z{Score: 1, Member: "uno"}).Err()
	if err != nil {
		panic(err)
	}
	err = client.ZAdd(ctx, key, redis.Z{Score: 2, Member: "two"}, redis.Z{Score: 3, Member: "three"}).Err()
	if err != nil {
		panic(err)
	}
	zrange, err := client.ZRangeWithScores(ctx, key, 0, -1).Result()
	if err != nil {
		panic(err)
	}
	expectedZRange := []redis.Z{
		{Score: 1, Member: "one"},
		{Score: 1, Member: "uno"},
		{Score: 2, Member: "two"},
		{Score: 3, Member: "three"},
	}
	if len(zrange) != len(expectedZRange) {
		panic("ZRANGE result length mismatch")
	}
	for i, z := range zrange {
		if z.Score != expectedZRange[i].Score || z.Member != expectedZRange[i].Member {
			panic(fmt.Sprintf("Expected %v, got %v", expectedZRange[i], z))
		}
	}
}

func hmsetT(ctx context.Context, client *redis.Client) {
	key := testutils.GenerateKey("hmsetT")
	err := client.HMSet(ctx, key, "field1", "Hello", "field2", "World").Err()
	if err != nil {
		testutils.Logger.Error(err, "Failed to HMSET")
		panic(err)
	}
	val1, err := client.HGet(ctx, key, "field1").Result()
	if err != nil || val1 != "Hello" {
		panic(fmt.Sprintf("Expected 'Hello', got '%s'", val1))
	}
	val2, err := client.HGet(ctx, key, "field2").Result()
	if err != nil || val2 != "World" {
		panic(fmt.Sprintf("Expected 'World', got '%s'", val2))
	}
}

func main() {
	flag.StringVar(&testutils.BackendAddr, "redis-proxy", "127.0.0.1:6379", "redis-proxy address")
	flag.StringVar(&testutils.ProxySrvAddr, "proxy-addr", "127.0.0.1:6378", "Proxy proxy address")
	flag.StringVar(&testutils.Username, "username", "nObPHzCQnwJ.admin", "Username")
	flag.StringVar(&testutils.Password, "password", "admin", "Password")
	flag.Parse()
	testutils.BuildAndRunProxySrv(testutils.BackendAddr)
	testutils.Logger.Info("Waiting for proxy proxy to start")
	time.Sleep(3 * time.Second)
	redisUrl := fmt.Sprintf("redis://%s:%s@%s", testutils.Username, testutils.Password, testutils.ProxySrvAddr)
	testutils.Logger.Info("Running client test connect to proxy proxy", "ProxySrvAddr", redisUrl)

	opt, err := redis.ParseURL(redisUrl)
	if err != nil {
		panic(err)
	}
	client := redis.NewClient(opt)
	defer func() {
		_ = client.Close()
	}()
	ctx := context.Background()
	hmsetT(ctx, client)
	zaddT(ctx, client)
	listT(ctx, client)
	multiT(ctx, client)
	testutils.Logger.Info("All tests passed successfully")
}

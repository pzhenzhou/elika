package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/pzhenzhou/elika/client-test/testutils"
	"github.com/redis/go-redis/v9"
	"sync"
	"time"
)

func main() {
	flag.StringVar(&testutils.BackendAddr, "redis-proxy", "127.0.0.1:6379", "redis-proxy address")
	flag.StringVar(&testutils.ProxySrvAddr, "proxy-addr", "127.0.0.1:6378", "Proxy proxy address")
	flag.StringVar(&testutils.Username, "username", "nObPHzCQnwJ.admin", "Username")
	flag.StringVar(&testutils.Password, "password", "admin", "Password")
	flag.IntVar(&testutils.BackendPoolSize, "backend-pool-size", 2, "Backend pool size")
	flag.Parse()
	testutils.BuildAndRunProxySrv(testutils.BackendAddr)
	testutils.Logger.Info("Waiting for proxy proxy to start")
	time.Sleep(3 * time.Second)
	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	// Start transaction client
	go runTransactionClient(ctx, &wg)
	// Start normal client
	go runNormalClient(ctx, &wg)
	wg.Wait()
	testutils.Logger.Info("Transaction cmd test completed")
}

func runTransactionClient(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	redisUrl := fmt.Sprintf("redis://%s:%s@%s", testutils.Username, testutils.Password, testutils.ProxySrvAddr)
	opt, err := redis.ParseURL(redisUrl)
	if err != nil {
		panic(err)
	}
	testutils.Logger.Info("Transaction client connecting to proxy proxy", "ProxySrvAddr", redisUrl)

	client := redis.NewClient(opt)
	defer client.Close()

	// Start transaction
	key1 := testutils.GenerateKey("tx1")
	key2 := testutils.GenerateKey("tx2")
	expectedVal1 := "tx1_value1"
	expectedVal2 := "tx2_value2"

	// Set initial values
	err = client.Set(ctx, key1, "0", 0).Err()
	if err != nil {
		testutils.Logger.Error(err, "Failed to set", "key1", key1)
		panic(err)
	}
	err = client.Set(ctx, key2, "0", 0).Err()
	if err != nil {
		panic(err)
	}

	testutils.Logger.Info("Starting transaction")

	// Use MULTI/EXEC for transaction
	pipe := client.TxPipeline()
	pipe.Set(ctx, key1, expectedVal1, 0)
	pipe.Set(ctx, key2, expectedVal2, 0)

	time.Sleep(2 * time.Second) // Simulate long transaction
	_, err = pipe.Exec(ctx)
	if err != nil {
		testutils.Logger.Error(err, "Transaction failed")
		panic(err)
	}

	// Verify transaction results
	val1, err := client.Get(ctx, key1).Result()
	if err != nil {
		testutils.Logger.Error(err, "Failed to get", "key1", key1)
		panic(err)
	}
	if val1 != expectedVal1 {
		err := fmt.Errorf("transaction verification failed for key1: expected %s, got %s", expectedVal1, val1)
		testutils.Logger.Error(err, "Transaction verification failed", "key1", key1,
			"value1", val1, "expectedVal1", expectedVal1)
		panic(err)
	}

	val2, err := client.Get(ctx, key2).Result()
	if err != nil {
		testutils.Logger.Error(err, "Failed to get", "key2", key2)
		panic(err)
	}
	if val2 != expectedVal2 {
		err := fmt.Errorf("transaction verification failed for key2: expected %s, got %s", expectedVal2, val2)
		testutils.Logger.Error(err, "Transaction verification failed",
			"key2", key2, "value2", val2, "expectedVal2", expectedVal2)
		panic(err)
	}

	testutils.Logger.Info("Transaction completed successfully",
		"key1", key1, "value1", val1,
		"key2", key2, "value2", val2)
}

func runNormalClient(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	time.Sleep(1 * time.Second) // Wait for transaction to start

	redisUrl := fmt.Sprintf("redis://%s:%s@%s", testutils.Username, testutils.Password, testutils.ProxySrvAddr)
	opt, err := redis.ParseURL(redisUrl)
	if err != nil {
		panic(err)
	}

	client := redis.NewClient(opt)
	defer client.Close()

	// Execute normal commands
	key := testutils.GenerateKey("normal")
	err = client.Set(ctx, key, "normal_value", 0).Err()
	if err != nil {
		panic(err)
	}

	val, err := client.Get(ctx, key).Result()
	if err != nil {
		panic(err)
	}
	testutils.Logger.Info("Normal command result", "key", key, "value", val)
}

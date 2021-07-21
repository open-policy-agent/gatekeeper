// Package main defines the hammer command for load testing gatekeeper installations.
//
// Cluster setup requirements:
// 1) Gatekeeper is installed
// 2) At least one ConstraintTemplate is installed
// 3) At least one Constraint is installed
//
// To run:
// $ hammer file
//
// file should be a YAML file which violates the constraint
//
// General load testing strategy:
//
// 1) Keep increasing --qps until it stops increasing.
// 2) If all requests are successful (the "Code | Count" table is empty),
//    increase --num-workers and go back to 1).
// 3) If some requests are no longer successful, you are at or near the maximum
//    QPS your setup of gatekeeper can support. Set QPS to above the highest
//    value of "Served QPS" you've seen. As you increase --num-workers, you
//    will see more failures as gatekeeper is unable to handle all requests.
//    The cluster's API Server may even be unable to even process requests. It
//    is in this state that you can test the behavior of the failure modes you're
//    looking for.
//
// Note that as you increase --num-workers, "Served QPS" will drop as gatekeeper
// will take longer to respond to requests the more pending requests pile up.
package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/errors"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/open-policy-agent/gatekeeper/cmd/gator/test"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	qps      *float64
	nWorkers *int

	duration   *time.Duration
	kubeconfig *string
)

func init() {
	rootCmd.AddCommand(test.Cmd)
	qps = rootCmd.Flags().Float64("qps", 50.0, "max QPS to send to the cluster")
	nWorkers = rootCmd.Flags().Int("num-workers", 300, "max number of simultaneous callers")

	duration = rootCmd.Flags().Duration("duration", time.Minute, "length of time to collect data")
	kubeconfig = rootCmd.Flags().String("kubeconfig", "~/.kube/config", "path to the kubeconfig file")
}

var rootCmd = &cobra.Command{
	Use: "hammer file",
	Short: `hammer sends requests to a cluster with the specified QPS

file should be the path to a YAML which is expected to fail at least one gatekeeper constraint on the cluster`,
	Example: `hammer disallowed.yaml

hammer disallowed.yaml --qps=500 --duration=5m`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		cmd.SilenceUsage = true

		if strings.HasPrefix(*kubeconfig, "~") {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			*kubeconfig = home + strings.TrimPrefix(*kubeconfig, "~")
		}

		cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			return err
		}
		// Explicitly set the Client to permit QPS rates above the user-desired QPS rate.
		cfg.QPS = 2 * float32(*qps)
		cfg.Burst = int(2 * cfg.QPS)

		c, err := client.New(cfg, client.Options{})
		if err != nil {
			return err
		}

		ctx := context.Background()

		work := make(chan struct{})
		results := make(chan result)
		stopWork := make(chan struct{})

		// Set up the results distribution and a worker to collate the data.
		var dist *distribution
		resultsWg := sync.WaitGroup{}
		resultsWg.Add(1)
		go func() {
			dist = collectDistribution(results)
			resultsWg.Done()
		}()

		// Create workers to consume work items.
		workerWg := sync.WaitGroup{}
		for i := 0; i < *nWorkers; i++ {
			workerWg.Add(1)
			go func() {
				worker(ctx, c, path, work, results)
				workerWg.Done()
			}()
		}

		// Start creating work items, then collect data for the specified time.
		addWork(*qps, work, stopWork)
		wait(*duration)

		// Send a message to stop creating new work items. All items currently
		// being processed will wait to complete.
		stopWork <- struct{}{}
		workerWg.Wait()

		close(results)
		resultsWg.Wait()

		// Print results.
		fmt.Println(dist.String())

		return nil
	},
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func wait(d time.Duration) {
	timer := time.NewTimer(d)
	i := 0
	ticker := time.NewTicker(time.Second)
	stop := false
	for {
		if stop {
			break
		}
		select {
		case <-ticker.C:
			i++
			fmt.Printf("\r%d", i)
		case <-timer.C:
			stop = true
		}
	}
	fmt.Println()
}

func worker(ctx context.Context, c client.Client, path string, work <-chan struct{}, times chan<- result) {
	u, err := read(path)
	if err != nil {
		panic(err)
	}

	for range work {
		start := time.Now()

		err = c.Create(ctx, u)
		if err == nil {
			// Either passed or skipped gatekeeper validation.
			// Should happen at most once, and only if the object is not already
			// on the cluster as calling Create() on an existing object is an error.
			times <- result{}
		}

		switch code := err.(errors.APIStatus).Status().Code; code {
		case 403:
			// Forbidden; failed gatekeeper validation.
			// This is what we want when we expect gatekeeper failures.
			times <- result{duration: time.Since(start)}
		default:
			times <- result{code: int(code)}
		}
	}
}

// addWork adds elements to work with frequency f.
// Stops adding new elements and closes the channel once stop is called.
func addWork(f float64, work chan<- struct{}, stop <-chan struct{}) {
	go func() {
		tick := time.Tick(time.Duration(float64(time.Second) / f))
		for {
			select {
			case <-stop:
				close(work)
				return
			case <-tick:
				work <- struct{}{}
			}
		}
	}()
}

func read(path string) (*unstructured.Unstructured, error) {
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	u := &unstructured.Unstructured{
		Object: map[string]interface{}{},
	}
	err = yaml.Unmarshal(bytes, u.Object)
	if err != nil {
		return nil, err
	}
	return u, nil
}

// collectDistribution collects a result channel into a distribution.
func collectDistribution(times <-chan result) *distribution {
	dist := &distribution{
		thresholds: make([]time.Duration, 30),
		counts:     make([]int, 30),
		errors:     make(map[int]int),
	}

	// Creates an approximately-logarithmic set of bins from 1 ms to 700 seconds.
	minDuration := time.Millisecond
	for i := 0; i < 30; i++ {
		d := minDuration * time.Duration(math.Pow10(i/5))
		switch i % 5 {
		case 0:
			dist.thresholds[i] = d
		case 1:
			dist.thresholds[i] = 2 * d
		case 2:
			dist.thresholds[i] = 3 * d
		case 3:
			dist.thresholds[i] = 5 * d
		case 4:
			dist.thresholds[i] = 7 * d
		}
	}

	for t := range times {
		dist.add(t)
	}
	return dist
}

// result is the outcome of sending a request to gatekeeper.
type result struct {
	// duration is how long it took to get a successful response.
	// Zero if the API Server returned an error.
	duration time.Duration
	// code is the error code returned by the API Server.
	code int
}

type distribution struct {
	// total is the running total of all 403 errors gatekeeper successfully served.
	total time.Duration

	thresholds []time.Duration
	counts     []int

	// errors are the non-403 errors we've gotten back from the API server.
	errors map[int]int
}

func (dst *distribution) add(r result) {
	if r.duration == 0 {
		dst.errors[r.code]++
		return
	}

	dst.total += r.duration
	for i, t := range dst.thresholds {
		if r.duration < t {
			dst.counts[i]++
			return
		}
	}

	// distribution should always have a bin large enough for the data.
	panic(fmt.Sprintf("Too large to fit in distribution: %v\n\n", r.duration))
}

// String prints the results collected in distribution in a human-readable format.
func (dst *distribution) String() string {
	w := strings.Builder{}
	total := 0

	w.WriteString("  Time | Count \n")
	w.WriteString("---------------\n")
	for i, c := range dst.counts {
		if c == 0 {
			continue
		}
		w.WriteString(fmt.Sprintf("%6v | %6d\n", dst.thresholds[i], c))
		total += c
	}
	w.WriteString("---------------\n")

	// Print any unexpected error codes.
	//   0 - We skipped gatekeeper validation and the API Server did not return
	//       another error.
	// 409 - We skipped gatekeeper validation and the API Server complained
	//       that the object already exists.
	// 429 - We got a generic "too many requests" response. This could be either
	//       from the API Server or from the cloud provider throttling requests.
	w.WriteString("  Code |  Count\n")
	for code, count := range dst.errors {
		w.WriteString(fmt.Sprintf("%6d | %6d\n", code, count))
	}
	w.WriteString("---------------\n")

	// Served QPS is the rate of requests which were handled by gatekeeper and returned the expected response.
	servedQPS := float64(total) / duration.Seconds()
	w.WriteString(fmt.Sprintf("Served QPS:   %7.3f/s\n", servedQPS))
	// Mean latency is an unweighted mean of responses which returned the expected 403 response.
	meanLatency := dst.total.Seconds() / float64(total)
	w.WriteString(fmt.Sprintf("Mean Latency: %7.3fs\n", meanLatency))

	return w.String()
}

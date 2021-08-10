// Copyright 2018 Anapaya Systems
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

/*
Package sintegration simplifies the creation of integration tests.

NOTE: this is a copy of github.com/scionproto/scion/go/lib/integration, with some omissions and modifications
*/
package sintegration

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/common"
	"github.com/scionproto/scion/go/lib/log"
	"github.com/scionproto/scion/go/lib/sciond"
	"github.com/scionproto/scion/go/lib/snet"
	"github.com/scionproto/scion/go/lib/util"
)

const (
	// StartServerTimeout is the timeout for starting a server.
	StartServerTimeout = 40 * time.Second
	// SCIONDAddressesFile is the default file for SCIOND addresses in a topology created
	// with the topology generator.
	SCIONDAddressesFile = "sciond_addresses.json"
)

type iaArgs []addr.IA

func (a iaArgs) String() string {
	rawIAs := make([]string, len(a))
	for i, ia := range a {
		rawIAs[i] = ia.String()
	}
	return strings.Join(rawIAs, ",")
}

// Set implements flag.Value.Set().
func (a *iaArgs) Set(value string) error {
	rawIAs := strings.Split(value, ",")
	for _, rawIA := range rawIAs {
		ia, err := addr.IAFromString(rawIA)
		if err != nil {
			return err
		}
		*a = append(*a, ia)
	}
	return nil
}

// Flags.
var (
	logConsole string
	srcIAs     iaArgs
	dstIAs     iaArgs
	outDir     string
)

// Integration can be used to run integration tests.
type Integration interface {
	// Name returns the name of the test
	Name() string
	// StartServer should start the server listening on the address dst.
	// StartServer should return after it is ready to accept clients.
	// The context should be used to make the server cancellable.
	StartServer(ctx context.Context, dst *snet.UDPAddr) (Waiter, error)
	// StartClient should start the client on the src address connecting to the dst address.
	// StartClient should return immediately.
	// The context should be used to make the client cancellable.
	StartClient(ctx context.Context, src, dst *snet.UDPAddr) (Waiter, error)
}

// Waiter is a descriptor of a process running in the integration test.
// It should be used to wait on completion of the process.
type Waiter interface {
	// Wait should block until the underlying program is terminated.
	Wait() error
}

// Init initializes the integration test, it adds and validates the command line flags,
// and initializes logging.
func Init() error {
	addTestFlags()
	if err := validateFlags(); err != nil {
		return err
	}
	return nil
}

// GenFile returns the path for the given file in the gen folder.
func GenFile(file string) string {
	return filepath.Join(outDir, "gen", file)
}

func addTestFlags() {
	flag.StringVar(&logConsole, "log.console", "info",
		"Console logging level: trace|debug|info|warn|error|crit")
	flag.Var(&srcIAs, "src", "Source ISD-ASes (comma separated list)")
	flag.Var(&dstIAs, "dst", "Destination ISD-ASes (comma separated list)")
	flag.StringVar(&outDir, "outDir", ".",
		"path to the output directory that contains gen and logs folders (default: .).")
}

func validateFlags() error {
	flag.Parse()
	logCfg := log.Config{Console: log.ConsoleConfig{Level: logConsole}}
	if err := log.Setup(logCfg); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s\n", err)
		flag.Usage()
		return err
	}
	var err error
	asList, err := util.LoadASList(GenFile("as_list.yml"))
	if err != nil {
		return err
	}
	if len(srcIAs) == 0 {
		srcIAs = asList.AllASes()
	}
	if len(dstIAs) == 0 {
		dstIAs = asList.AllASes()
	}
	return nil
}

// IAPair is a source, destination pair. The client (Src) will dial the server (Dst).
type IAPair struct {
	Src, Dst *snet.UDPAddr
}

// IAPairs returns all IAPairs that should be tested.
func IAPairs(hostAddr HostAddr) []IAPair {
	return generateAllSrcDst(hostAddr, false)
}

func generateSrcDst(hostAddr HostAddr) ([]*snet.UDPAddr, []*snet.UDPAddr) {
	srcASes := make([]*snet.UDPAddr, 0, len(srcIAs))
	for _, src := range srcIAs {
		srcASes = append(srcASes, hostAddr(src))
	}
	dstASes := make([]*snet.UDPAddr, 0, len(dstIAs))
	for _, dst := range dstIAs {
		dstASes = append(dstASes, hostAddr(dst))
	}
	shuffle(len(srcASes), func(i, j int) {
		srcASes[i], srcASes[j] = srcASes[j], srcASes[i]
	})
	shuffle(len(dstASes), func(i, j int) {
		dstASes[i], dstASes[j] = dstASes[j], dstASes[i]
	})
	return srcASes, dstASes
}

// generateAllSrcDst generates the cartesian product shuffle(srcASes) x shuffle(dstASes).
// It omits pairs where srcAS==dstAS, if unique is true.
func generateAllSrcDst(hostAddr HostAddr, unique bool) []IAPair {
	srcASes, dstASes := generateSrcDst(hostAddr)
	pairs := make([]IAPair, 0, len(srcASes)*len(dstASes))
	for _, src := range srcASes {
		for _, dst := range dstASes {
			if !unique || !src.IA.Equal(dst.IA) {
				pairs = append(pairs, IAPair{src, dst})
			}
		}
	}
	return pairs
}

type HostAddr func(ia addr.IA) *snet.UDPAddr

// interface kept similar to go 1.10
func shuffle(n int, swap func(i, j int)) {
	for i := n - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		swap(i, j)
	}
}

// WithTimestamp returns s with the now timestamp prefixed.
// This is helpful for logging staments to stdout/stderr or in a file where the logger isn't used.
func WithTimestamp(s string) string {
	return fmt.Sprintf("%v %s", time.Now().UTC().Format(common.TimeFmt), s)
}

// RunClient runs a client on the given IAPair.
// If the client does not finish until timeout it is killed.
func RunClient(in Integration, pair IAPair, timeout time.Duration) error {

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c, err := in.StartClient(ctx, pair.Src, pair.Dst)
	if err != nil {
		return err
	}
	if err = c.Wait(); err != nil {
		return err
	}
	return nil
}

// ExecuteTimed executes f and prints how long f took to StdOut. Returns the error of f.
func ExecuteTimed(name string, f func() error) error {
	start := time.Now()
	err := f()
	elapsed := time.Since(start)

	// XXX(roosd) This string is used by buildkite to group output blocks.
	fmt.Printf("--- test results: %s\n", name)
	if err != nil {
		log.Error("Test failed", "name", name, "elapsed", elapsed)
		return err
	}
	log.Info("Test successful", "name", name, "elapsed", elapsed)
	return err
}

// ExtractUniqueDsts returns all unique destinations in pairs.
func ExtractUniqueDsts(pairs []IAPair) []*snet.UDPAddr {
	uniqueDsts := make(map[*snet.UDPAddr]bool)
	var res []*snet.UDPAddr
	for _, pair := range pairs {
		if !uniqueDsts[pair.Dst] {
			res = append(res, pair.Dst)
			uniqueDsts[pair.Dst] = true
		}
	}
	return res
}

func GetSCIONDAddresses(networksFile string) (map[string]string, error) {
	b, err := ioutil.ReadFile(networksFile)
	if err != nil {
		return nil, err
	}

	var networks map[string]string
	err = json.Unmarshal(b, &networks)
	if err != nil {
		return nil, err
	}
	return networks, nil
}

func GetSCIONDAddress(networksFile string, ia addr.IA) (string, error) {
	addresses, err := GetSCIONDAddresses(networksFile)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("[%v]:%d", addresses[ia.String()], sciond.DefaultAPIPort), nil
}
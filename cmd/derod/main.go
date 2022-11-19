// Copyright 2017-2021 DERO Project. All rights reserved.
// Use of this source code in any form is governed by RESEARCH license.
// license can be found in the LICENSE file.
// GPG: 0F39 E425 8C65 3947 702A  8234 08B2 0360 A03A 9DE8
//
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY
// EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL
// THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
// PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
// STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF
// THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/blockchain"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/p2p"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/transaction"
	"github.com/docopt/docopt-go"
	"github.com/go-logr/logr"
	"gopkg.in/natefinch/lumberjack.v2"

	//import "crypto/sha1"

	//import "golang.org/x/crypto/sha3"

	derodrpc "github.com/deroproject/derohe/cmd/derod/rpc"
	"github.com/deroproject/derohe/cryptography/crypto"
)

var command_line string = `derod 
DERO : A secure, private blockchain with smart-contracts

Usage:
  derod [--help] [--version] [--testnet] [--debug]  [--sync-node] [--timeisinsync] [--fastsync] [--socks-proxy=<socks_ip:port>] [--data-dir=<directory>] [--p2p-bind=<0.0.0.0:18089>] [--add-exclusive-node=<ip:port>]... [--add-priority-node=<ip:port>]... [--min-peers=<11>] [--max-peers=<100>] [--rpc-bind=<127.0.0.1:9999>] [--getwork-bind=<0.0.0.0:18089>] [--node-tag=<unique name>] [--prune-history=<50>] [--integrator-address=<address>] [--clog-level=1] [--flog-level=1]
  derod -h | --help
  derod --version

Options:
  -h --help     Show this screen.
  --version     Show version.
  --testnet  	Run in testnet mode.
  --debug       Debug mode enabled, print more log messages
  --clog-level=1	Set console log level (0 to 127) 
  --flog-level=1	Set file log level (0 to 127)
  --fastsync      Fast sync mode (this option has effect only while bootstrapping)
  --timeisinsync  Confirms to daemon that time is in sync, so daemon doesn't try to sync
  --socks-proxy=<socks_ip:port>  Use a proxy to connect to network.
  --data-dir=<directory>    Store blockchain data at this location
  --rpc-bind=<127.0.0.1:9999>    RPC listens on this ip:port
  --p2p-bind=<0.0.0.0:18089>    p2p server listens on this ip:port, specify port 0 to disable listening server
  --getwork-bind=<0.0.0.0:10100>    getwork server listens on this ip:port, specify port 0 to disable listening server
  --add-exclusive-node=<ip:port>	Connect to specific peer only 
  --add-priority-node=<ip:port>	Maintain persistant connection to specified peer
  --sync-node       Sync node automatically with the seeds nodes. This option is for rare use.
  --node-tag=<unique name>	Unique name of node, visible to everyone
  --integrator-address	if this node mines a block,Integrator rewards will be given to address.default is dev's address.
  --min-peers=<31>	  Node will try to maintain atleast this many connections to peers
  --max-peers=<101>	  Node will maintain maximim this many connections to peers and will stop accepting connections
  --prune-history=<50>	prunes blockchain history until the specific topo_height

  `

// adding some colors
var green string = "\033[32m"      // default is green color
var yellow string = "\033[33m"     // make prompt yellow
var red string = "\033[31m"        // make prompt red
var blue string = "\033[34m"       // blue color
var reset_color string = "\033[0m" // reset color

var Exit_In_Progress = make(chan bool)

var logger logr.Logger

func save_config_file() {

	config_file := filepath.Join(globals.GetDataDirectory(), "config.json")
	file, err := os.Create(config_file)
	if err != nil {
		logger.Error(err, "creating new config file")
	} else {
		defer file.Close()
		encoder := json.NewEncoder(file)
		encoder.SetIndent("", "\t")
		err = encoder.Encode(&config.RunningConfig)
		if err != nil {
			logger.Error(err, "Error loading config data")
		} else { // successfully unmarshalled data
			logger.V(1).Info("Successfully saved config to file")
		}
	}
}

// loads peers list from disk
func load_config_file() {

	config_file := filepath.Join(globals.GetDataDirectory(), "config.json")
	if _, err := os.Stat(config_file); errors.Is(err, os.ErrNotExist) {
		return // since file doesn't exist , we cannot load it
	}
	file, err := os.Open(config_file)
	if err != nil {
		logger.Error(err, "opening config file")
	} else {
		defer file.Close()
		decoder := json.NewDecoder(file)
		err = decoder.Decode(&config.RunningConfig)
		if err != nil {
			logger.Error(err, "Error loading config from file")
		} else { // successfully loaded
			logger.V(1).Info("Successfully loaded config from file")

			// Set additional running variables based on config

			p2p.Min_Peers = config.RunningConfig.Min_Peers
			p2p.Max_Peers = config.RunningConfig.Max_Peers

			if len(config.RunningConfig.NodeTag) > 0 {
				p2p.SetNodeTag(config.RunningConfig.NodeTag)
			}
		}
	}

}

func dump(filename string) {
	f, err := os.Create(filename)
	if err != nil {
		fmt.Printf("err creating file %s\n", err)
		return
	}

	runtime.GC()
	debug.WriteHeapDump(f.Fd())

	err = f.Close()
	if err != nil {
		fmt.Printf("err closing file %s\n", err)
	}
}

var threadStartCount int
var mutexStartCount int
var blockingStartCount int
var goStartCount int
var logfile *lumberjack.Logger

func main() {

	// Setting upper limit to 100,000 to avoid immediate crashes
	debug.SetMaxThreads(100000)

	runtime.MemProfileRate = 0
	var err error
	globals.Arguments, err = docopt.Parse(command_line, nil, true, config.Version.String(), false)

	threadStartCount = globals.CountThreads()
	mutexStartCount = globals.CountMutex()
	blockingStartCount = globals.CountBlocked()
	goStartCount = globals.CountGoProcs()

	if err != nil {
		fmt.Printf("Error while parsing options err: %s\n", err)
		return
	}

	// We need to initialize readline first, so it changes stderr to ansi processor on windows

	l, err := readline.NewEx(&readline.Config{
		//Prompt:          "\033[92mDERO:\033[32m»\033[0m",
		Prompt:          "\033[92mDERO:\033[32m>>>\033[0m ",
		HistoryFile:     filepath.Join(os.TempDir(), "derod_readline.tmp"),
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",

		HistorySearchFold:   true,
		FuncFilterInputRune: filterInput,
	})
	if err != nil {
		fmt.Printf("Error starting readline err: %s\n", err)
		return
	}
	defer l.Close()

	// parse arguments and setup logging , print basic information
	exename, _ := os.Executable()
	globals.InitializeLog(l.Stdout(), &lumberjack.Logger{
		Filename:   exename + ".log",
		MaxSize:    100, // megabytes
		MaxBackups: 2,
	})

	logger = globals.Logger.WithName("derod")

	load_config_file() // load config file

	logger.Info("DERO HE daemon :  It is an alpha version, use it for testing/evaluations purpose only.")
	logger.Info("Copyright 2017-2021 DERO Project. All rights reserved.")
	logger.Info("", "OS", runtime.GOOS, "ARCH", runtime.GOARCH, "GOMAXPROCS", runtime.GOMAXPROCS(0))
	logger.Info("", "Version", config.Version.String())

	logger.V(1).Info("", "Arguments", globals.Arguments)

	globals.Initialize() // setup network and proxy

	logger.V(0).Info("", "MODE", globals.Config.Name)
	logger.V(0).Info("", "Daemon data directory", globals.GetDataDirectory())

	//go check_update_loop ()

	params := map[string]interface{}{}

	// check  whether we are pruning, if requested do so
	prune_topo := int64(50)
	if _, ok := globals.Arguments["--prune-history"]; ok && globals.Arguments["--prune-history"] != nil { // user specified a limit, use it if possible
		i, err := strconv.ParseInt(globals.Arguments["--prune-history"].(string), 10, 64)
		if err != nil {
			logger.Error(err, "error Parsing --prune-history ")
			return
		} else {
			if i <= 1 {
				logger.Error(fmt.Errorf("--prune-history should be positive and more than 1"), "invalid argument")
				return
			} else {
				prune_topo = i
			}
		}
		logger.Info("will prune history till", "topo_height", prune_topo)

		if err := blockchain.Prune_Blockchain(prune_topo); err != nil {
			logger.Error(err, "Error pruning blockchain ")
			return
		} else {
			logger.Info("blockchain pruning successful")

		}
	}

	if _, ok := globals.Arguments["--timeisinsync"]; ok {
		globals.TimeIsInSync = globals.Arguments["--timeisinsync"].(bool)
	}

	if _, ok := globals.Arguments["--integrator-address"]; ok {
		params["--integrator-address"] = globals.Arguments["--integrator-address"]
	}

	chain, err := blockchain.Blockchain_Start(params)
	if err != nil {
		logger.Error(err, "Error starting blockchain")
		return
	}

	params["chain"] = chain

	// since user is using a proxy, he definitely does not want to give out his IP
	if globals.Arguments["--socks-proxy"] != nil {
		globals.Arguments["--p2p-bind"] = ":0"
		logger.Info("Disabling P2P server since we are using socks proxy")
	}

	p2p.P2P_Init(params)
	rpcserver, _ := derodrpc.RPCServer_Start(params)

	i, err := strconv.ParseInt(os.Getenv("JOB_SEND_TIME_DELAY"), 10, 64)
	if err != nil && i > 0 {
		config.RunningConfig.GETWorkJobDispatchTime = time.Duration(i * int64(time.Millisecond))
	}

	go derodrpc.Getwork_server()

	// setup function pointers
	chain.P2P_Block_Relayer = func(cbl *block.Complete_Block, peerid uint64) {
		p2p.Broadcast_Block(cbl, peerid)
	}

	chain.P2P_MiniBlock_Relayer = func(mbl block.MiniBlock, peerid uint64) {
		p2p.Broadcast_MiniBlock(mbl, peerid)
	}

	{
		current_blid, err := chain.Load_Block_Topological_order_at_index(17600)
		if err == nil {

			current_blid := current_blid
			for {
				height := chain.Load_Height_for_BL_ID(current_blid)

				if height < 17500 {
					break
				}

				r, err := chain.Store.Topo_store.Read(int64(height))
				if err != nil {
					panic(err)
				}
				if r.BLOCK_ID != current_blid {
					fmt.Printf("Fixing corruption r %+v  , current_blid %s current_blid_height %d\n", r, current_blid, height)

					fix_commit_version, err := chain.ReadBlockSnapshotVersion(current_blid)
					if err != nil {
						panic(err)
					}

					chain.Store.Topo_store.Write(int64(height), current_blid, fix_commit_version, int64(height))

				}

				fix_bl, err := chain.Load_BL_FROM_ID(current_blid)
				if err != nil {
					panic(err)
				}
				current_blid = fix_bl.Tips[0]
			}
		}
	}
	globals.Cron.Start() // start cron jobs

	globals.Cron.AddFunc("@every 10s", p2p.UpdateLiveBlockData)
	// This tiny goroutine continuously updates status as required

	// go func() {
	// 	for {
	// 		time.Sleep(1 * time.Minute)
	// 		RunDiagnosticCheckSquence(chain, l)
	// 	}
	// }()

	last_our_height := int64(0)
	last_best_height := int64(0)
	last_peer_count := uint64(0)
	last_topo_height := int64(0)
	last_mempool_tx_count := 0
	last_regpool_tx_count := 0
	last_second := int64(0)
	our_height := chain.Get_Height()
	best_height, best_topo_height := p2p.Best_Peer_Height()
	peer_count := p2p.Peer_Count()
	topo_height := chain.Load_TOPO_HEIGHT()
	peer_whitelist := p2p.Peer_Count_Whitelist()

	mempool_tx_count := len(chain.Mempool.Mempool_List_TX())
	regpool_tx_count := len(chain.Regpool.Regpool_List_TX())

	network_hashrate := chain.Get_Network_HashRate()
	hash_rate_string := hashratetostring(network_hashrate)

	miniblock_count := chain.MiniBlocks.Count()
	miner_count := derodrpc.CountMiners()

	total_orphans := p2p.CountNetworkOrphanSince(uint64(chain.Get_Height() - config.RunningConfig.NetworkStatsKeepCount))
	my_orphan_blocks_count := globals.CountOrphanBlocks + globals.CountOrphanMinis

	network_loss := float64(0)
	blockcount := config.RunningConfig.NetworkStatsKeepCount * 10

	testnet_string := ""
	if globals.IsMainnet() {
		testnet_string = "\033[31m MAINNET"
	} else {
		testnet_string = "\033[31m TESTNET"
	}

	// 0.5 second sleeps
	go func() {
		for {
			select {
			case <-Exit_In_Progress:
				return
			default:
			}

			func() {
				defer globals.Recover(0) // a panic might occur, due to some rare file system issues, so skip them
				our_height = chain.Get_Height()
				best_height, best_topo_height = p2p.Best_Peer_Height()
				peer_count = p2p.Peer_Count()
				topo_height = chain.Load_TOPO_HEIGHT()
				peer_whitelist = p2p.Peer_Count_Whitelist()

				mempool_tx_count = len(chain.Mempool.Mempool_List_TX())
				regpool_tx_count = len(chain.Regpool.Regpool_List_TX())
				network_hashrate = chain.Get_Network_HashRate()
				hash_rate_string = hashratetostring(network_hashrate)

				miniblock_count = chain.MiniBlocks.Count()
				miner_count = derodrpc.CountMiners()
				total_orphans = p2p.CountNetworkOrphanSince(uint64(chain.Get_Height() - config.RunningConfig.NetworkStatsKeepCount))
				my_orphan_blocks_count = globals.CountOrphanBlocks + globals.CountOrphanMinis

				network_loss = float64(0)
				blockcount = config.RunningConfig.NetworkStatsKeepCount * 10
				if globals.CountTotalBlocks < blockcount {
					blockcount = globals.CountTotalBlocks
				} else {
					blockcount += int64(total_orphans)
				}

				if total_orphans > 0 && blockcount > 0 {
					network_loss = float64(float64(total_orphans)/float64(blockcount)) * 100
				}

				if globals.BlockChainStartHeight == 0 && globals.CountTotalBlocks >= 1 {
					globals.BlockChainStartHeight = chain.Get_Height()
				}

			}()
			time.Sleep(500 * time.Millisecond)
		}
	}()

	go func() {

		for {
			select {
			case <-Exit_In_Progress:
				return
			default:
			}

			if last_second != time.Now().Unix() || last_our_height != our_height || last_best_height != best_height || last_peer_count != peer_count || last_topo_height != topo_height || last_mempool_tx_count != mempool_tx_count || last_regpool_tx_count != regpool_tx_count {
				// choose color based on urgency
				color := "\033[32m" // default is green color
				if our_height < best_height {
					color = "\033[33m" // make prompt yellow
					globals.NetworkTurtle = true
				} else if our_height > best_height {
					color = "\033[31m" // make prompt red
					globals.NetworkTurtle = false
				}

				pcolor := "\033[32m" // default is green color
				if peer_count < 1 {
					pcolor = "\033[31m" // make prompt red
					globals.NetworkTurtle = false
				} else if peer_count <= 8 {
					pcolor = "\033[33m" // make prompt yellow
					globals.NetworkTurtle = true
				}

				turtle_string := ""
				if globals.NetworkTurtle {
					turtle_string = " (\033[31mTurtle\033[32m)"
				}

				if config.RunningConfig.OnlyTrusted {
					turtle_string = " (\033[31mTrusted Mode\033[32m)"
					if globals.NetworkTurtle {
						turtle_string = turtle_string + " (!)"
					}
				}

				menu_string := testnet_string + fmt.Sprintf(" %d/%d (%.1f%%) %s|%s|%s", globals.MiniBlocksCollectionCount, miniblock_count, network_loss, globals.GetOffset().Round(time.Millisecond).String(), globals.GetOffsetNTP().Round(time.Millisecond).String(), globals.GetOffsetP2P().Round(time.Millisecond).String())

				good_blocks := (globals.CountMinisAccepted + globals.CountBlocksAccepted)
				unique_miner_count := globals.CountUniqueMiners

				l.SetPrompt(fmt.Sprintf("\033[1m\033[32mDERO HE (\033[31m%s-mod\033[32m):%s \033[0m"+color+"%d/%d [%d/%d] "+pcolor+"P %d/%d TXp %d:%d \033[32mNW %s >MN %d/%d [%d/%d] %s>>\033[0m ",
					config.RunningConfig.OperatorName, turtle_string, our_height, topo_height, best_height, best_topo_height, peer_whitelist, peer_count, mempool_tx_count,
					regpool_tx_count, hash_rate_string, unique_miner_count, miner_count, (good_blocks - my_orphan_blocks_count), good_blocks, menu_string))
				l.Refresh()
				last_second = time.Now().Unix()
				last_our_height = our_height
				last_best_height = best_height
				last_peer_count = peer_count
				last_mempool_tx_count = mempool_tx_count
				last_regpool_tx_count = regpool_tx_count
				last_topo_height = best_topo_height

			}

			time.Sleep(100 * time.Millisecond)
		}
	}()

	setPasswordCfg := l.GenPasswordConfig()
	setPasswordCfg.SetListener(func(line []rune, pos int, key rune) (newLine []rune, newPos int, ok bool) {
		l.SetPrompt(fmt.Sprintf("Enter password(%v): ", len(line)))
		l.Refresh()
		return nil, 0, false
	})
	l.Refresh() // refresh the prompt

	go func() {
		var gracefulStop = make(chan os.Signal, 1)
		signal.Notify(gracefulStop, os.Interrupt) // listen to all signals
		for {
			sig := <-gracefulStop
			logger.Info("received signal", "signal", sig)
			if sig.String() == "interrupt" {
				close(Exit_In_Progress)
				return
			}
		}
	}()

	for {
		if err = readline_loop(l, chain, logger); err == nil {
			break
		}
	}

	logger.Info("Exit in Progress, Please wait")
	time.Sleep(100 * time.Millisecond) // give prompt update time to finish

	rpcserver.RPCServer_Stop()
	p2p.P2P_Shutdown() // shutdown p2p subsystem
	chain.Shutdown()   // shutdown chain subsysem

	for globals.Subsystem_Active > 0 {
		logger.Info("Exit in Progress, Please wait.", "active subsystems", globals.Subsystem_Active)
		time.Sleep(1000 * time.Millisecond)
	}
}

func readline_loop(l *readline.Instance, chain *blockchain.Blockchain, logger logr.Logger) (err error) {

	defer func() {
		if r := recover(); r != nil {
			logger.V(0).Error(nil, "Recovered ", "error", r)
			err = fmt.Errorf("crashed")
		}

	}()

restart_loop:
	for {
		line, err := l.Readline()
		if err == io.EOF {
			logger.Info("Ctrl-d received, to exit type - exit")
			continue
		}

		if err == readline.ErrInterrupt {
			if len(line) == 0 {
				logger.Info("Ctrl-C received, to exit type - exit")
				// return nil
			} else {
				continue
			}
		}

		line = strings.TrimSpace(line)
		line_parts := strings.Fields(line)

		command := ""
		if len(line_parts) >= 1 {
			command = strings.ToLower(line_parts[0])
		}

		switch {
		case line == "help":
			usage(l.Stdout())

		case command == "profile": // writes cpu and memory profile
			// TODO enable profile over http rpc to enable better testing/tracking
			cpufile, err := os.Create(filepath.Join(globals.GetDataDirectory(), "cpuprofile.prof"))
			if err != nil {
				logger.Error(err, "Could not start cpu profiling.")
				continue
			}
			if err := pprof.StartCPUProfile(cpufile); err != nil {
				logger.Error(err, "could not start CPU profile")
				continue
			}
			logger.Info("CPU profiling will be available after program exits.", "path", filepath.Join(globals.GetDataDirectory(), "cpuprofile.prof"))
			defer pprof.StopCPUProfile()

			/*
				        	memoryfile,err := os.Create(filepath.Join(globals.GetDataDirectory(), "memoryprofile.prof"))
							if err != nil{
								globals.Logger.Errorf("Could not start memory profiling, err %s", err)
								continue
							}
							if err := pprof.WriteHeapProfile(memoryfile); err != nil {
				            	globals.Logger.Warnf("could not start memory profile: ", err)
				        	}
				        	memoryfile.Close()
			*/

		case command == "setintegratoraddress":
			if len(line_parts) != 2 {
				logger.Error(fmt.Errorf("This function requires 1 parameters, dero address"), "")
				continue
			}
			if addr, err := rpc.NewAddress(line_parts[1]); err != nil {
				logger.Error(err, "invalid address")
				continue
			} else {
				chain.SetIntegratorAddress(*addr)
				logger.Info("will use", "integrator_address", chain.IntegratorAddress().String())
			}

		case command == "print_bc":

			logger.Info("printing block chain")
			// first is starting point, second is ending point
			start := int64(0)
			stop := int64(0)

			if len(line_parts) != 3 {
				logger.Error(fmt.Errorf("This function requires 2 parameters, start and endpoint"), "")
				continue
			}
			if s, err := strconv.ParseInt(line_parts[1], 10, 64); err == nil {
				start = s
			} else {
				logger.Error(err, "Invalid start value", "value", line_parts[1])
				continue
			}

			if s, err := strconv.ParseInt(line_parts[2], 10, 64); err == nil {
				stop = s
			} else {
				logger.Error(err, "Invalid stop value", "value", line_parts[1])
				continue
			}

			if start < 0 || start > int64(chain.Load_TOPO_HEIGHT()) {
				logger.Error(fmt.Errorf("Start value should be be between 0 and current height"), "")
				continue
			}
			if start > stop || stop > int64(chain.Load_TOPO_HEIGHT()) {
				logger.Error(fmt.Errorf("Stop value should be > start and current height"), "")
				continue
			}

			logger.Info("Printing block chain", "start", start, "stop", stop)

			for i := start; i <= stop; i++ {
				// get block id at height
				current_block_id, err := chain.Load_Block_Topological_order_at_index(i)
				if err != nil {
					logger.Error(err, "Skipping block at height due to error \n", "height", i)
					continue
				}
				var timestamp uint64
				diff := new(big.Int)
				if chain.Block_Exists(current_block_id) {
					timestamp = chain.Load_Block_Timestamp(current_block_id)
					diff = chain.Load_Block_Difficulty(current_block_id)
				}

				version, err := chain.ReadBlockSnapshotVersion(current_block_id)
				if err != nil {
					panic(err)
				}

				balance_hash, err := chain.Load_Merkle_Hash(version)

				if err != nil {
					panic(err)
				}

				logger.Info("", "topo height", i, "height", chain.Load_Height_for_BL_ID(current_block_id), "timestamp", timestamp, "difficulty", diff.String())
				logger.Info("", "Block Id", current_block_id.String(), "balance_tree hash", balance_hash.String())
				logger.Info("\n")

			}
		case command == "regpool_print":
			chain.Regpool.Regpool_Print()

		case command == "regpool_flush":
			chain.Regpool.Regpool_flush()
		case command == "regpool_delete_tx":

			if len(line_parts) == 2 && len(line_parts[1]) == 64 {
				txid, err := hex.DecodeString(strings.ToLower(line_parts[1]))
				if err != nil {
					logger.Error(err, "err parsing txid")
					continue
				}
				var hash crypto.Hash
				copy(hash[:32], []byte(txid))

				chain.Regpool.Regpool_Delete_TX(hash)
			} else {
				logger.Error(fmt.Errorf("regpool_delete_tx  needs a single transaction id as argument"), "")
			}

		case command == "mempool_dump": // dump mempool to directory
			tx_hash_list_sorted := chain.Mempool.Mempool_List_TX_SortedInfo() // hash of all tx expected to be included within this block , sorted by fees

			os.Mkdir(filepath.Join(globals.GetDataDirectory(), "mempool"), 0755)
			count := 0
			for _, txi := range tx_hash_list_sorted {
				if tx := chain.Mempool.Mempool_Get_TX(txi.Hash); tx != nil {
					os.WriteFile(filepath.Join(globals.GetDataDirectory(), "mempool", txi.Hash.String()), tx.Serialize(), 0755)
					count++
				}
			}
			logger.Info("flushed mempool to driectory", "count", count, "dir", filepath.Join(globals.GetDataDirectory(), "mempool"))

		case command == "mempool_print":
			chain.Mempool.Mempool_Print()

		case command == "mempool_flush":
			chain.Mempool.Mempool_flush()
		case command == "mempool_delete_tx":

			if len(line_parts) == 2 && len(line_parts[1]) == 64 {
				txid, err := hex.DecodeString(strings.ToLower(line_parts[1]))
				if err != nil {
					logger.Error(err, "err parsing txid")
					continue
				}
				var hash crypto.Hash
				copy(hash[:32], []byte(txid))

				chain.Mempool.Mempool_Delete_TX(hash)
			} else {
				logger.Error(fmt.Errorf("mempool_delete_tx  needs a single transaction id as argument"), "")
			}

		case command == "version":
			logger.Info("", "OS", runtime.GOOS, "ARCH", runtime.GOARCH, "GOMAXPROCS", runtime.GOMAXPROCS(0))
			logger.Info("", "Version", config.Version.String())

		case command == "print_tree": // prints entire block chain tree
			//WriteBlockChainTree(chain, "/tmp/graph.dot")

		case command == "block_export":
			var hash crypto.Hash

			if len(line_parts) == 2 && len(line_parts[1]) == 64 {
				bl_raw, err := hex.DecodeString(strings.ToLower(line_parts[1]))
				if err != nil {
					fmt.Printf("err while decoding blid err %s\n", err)
					continue
				}
				copy(hash[:32], []byte(bl_raw))
			} else {
				fmt.Printf("block_export  needs a single block id as argument\n")
				continue
			}

			var cbl *block.Complete_Block

			bl, err := chain.Load_BL_FROM_ID(hash)
			if err != nil {
				fmt.Printf("Err %s\n", err)
				continue
			}
			cbl = &block.Complete_Block{Bl: bl}
			for _, txid := range bl.Tx_hashes {

				var tx transaction.Transaction
				if tx_bytes, err := chain.Store.Block_tx_store.ReadTX(txid); err != nil {
					fmt.Printf("err while reading txid err %s\n", err)
					continue restart_loop
				} else if err = tx.Deserialize(tx_bytes); err != nil {
					fmt.Printf("err deserializing tx err %s\n", err)
					continue restart_loop
				}
				cbl.Txs = append(cbl.Txs, &tx)

			}

			cbl_bytes := p2p.Convert_CBL_TO_P2PCBL(cbl, true)

			if err := os.WriteFile(fmt.Sprintf("/tmp/%s.block", hash), cbl_bytes, 0755); err != nil {
				fmt.Printf("err writing block %s\n", err)
				continue
			}

			fmt.Printf("successfully exported block to %s\n", fmt.Sprintf("/tmp/%s.block", hash))

		case command == "block_import":
			var hash crypto.Hash

			if len(line_parts) == 2 && len(line_parts[1]) == 64 {
				bl_raw, err := hex.DecodeString(strings.ToLower(line_parts[1]))
				if err != nil {
					fmt.Printf("err while decoding blid err %s\n", err)
					continue
				}
				copy(hash[:32], []byte(bl_raw))
			} else {
				fmt.Printf("install_block  needs a single block id as argument\n")
				continue
			}

			var cbl *block.Complete_Block

			if block_data, err := os.ReadFile(fmt.Sprintf("/tmp/%s.block", hash)); err == nil {

				cbl = p2p.Convert_P2PCBL_TO_CBL(block_data)
			} else {
				fmt.Printf("err reading block %s\n", err)
				continue
			}

			err, _ = chain.Add_Complete_Block(cbl)
			fmt.Printf("err adding block %s\n", err)

		case command == "fix":
			tips := chain.Get_TIPS()

			current_blid := tips[0]
			for {
				height := chain.Load_Height_for_BL_ID(current_blid)

				//fmt.Printf("checking height %d\n", height)

				if height < 1 {
					break
				}

				r, err := chain.Store.Topo_store.Read(int64(height))
				if err != nil {
					panic(err)
				}
				if r.BLOCK_ID != current_blid {
					fmt.Printf("corruption due to XYZ r %+v  , current_blid %s current_blid_height %d\n", r, current_blid, height)

					fix_commit_version, err := chain.ReadBlockSnapshotVersion(current_blid)
					if err != nil {
						panic(err)
					}

					chain.Store.Topo_store.Write(int64(height), current_blid, fix_commit_version, int64(height))

				}

				fix_bl, err := chain.Load_BL_FROM_ID(current_blid)
				if err != nil {
					panic(err)
				}

				current_blid = fix_bl.Tips[0]

				/*		fix_commit_version, err = chain.ReadBlockSnapshotVersion(current_block_id)
						if err != nil {
							panic(err)
						}
				*/

			}

		case command == "print_block":

			fmt.Printf("printing block\n")
			var hash crypto.Hash

			if len(line_parts) == 2 && len(line_parts[1]) == 64 {
				bl_raw, err := hex.DecodeString(strings.ToLower(line_parts[1]))
				if err != nil {
					fmt.Printf("err while decoding blid err %s\n", err)
					continue
				}
				copy(hash[:32], []byte(bl_raw))
			} else if len(line_parts) == 2 {
				if s, err := strconv.ParseInt(line_parts[1], 10, 64); err == nil {
					_ = s
					// first load block id from topo height

					hash, err = chain.Load_Block_Topological_order_at_index(s)
					if err != nil {
						fmt.Printf("Skipping block at topo height %d due to error %s\n", s, err)
						continue
					}
				}
			} else {
				fmt.Printf("print_block  needs a single block id as argument\n")
				continue
			}
			bl, err := chain.Load_BL_FROM_ID(hash)
			if err != nil {
				fmt.Printf("Err %s\n", err)
				continue
			}

			header, _ := derodrpc.GetBlockHeader(chain, hash)
			fmt.Fprintf(os.Stdout, "BLID:%s\n", bl.GetHash())
			fmt.Fprintf(os.Stdout, "Major version:%d Minor version: %d ", bl.Major_Version, bl.Minor_Version)
			fmt.Fprintf(os.Stdout, "Height:%d ", bl.Height)
			fmt.Fprintf(os.Stdout, "Timestamp:%d  (%s)\n", bl.Timestamp, bl.GetTimestamp())
			for i := range bl.Tips {
				fmt.Fprintf(os.Stdout, "Past %d:%s\n", i, bl.Tips[i])
			}
			for i, mbl := range bl.MiniBlocks {
				fmt.Fprintf(os.Stdout, "Mini %d:%s %s\n", i, mbl, header.Miners[i])
			}
			for i, txid := range bl.Tx_hashes {
				fmt.Fprintf(os.Stdout, "tx %d:%s\n", i, txid)
			}

			fmt.Printf("difficulty: %s\n", chain.Load_Block_Difficulty(hash).String())
			fmt.Printf("TopoHeight: %d\n", chain.Load_Block_Topological_order(hash))

			version, err := chain.ReadBlockSnapshotVersion(hash)
			if err != nil {
				panic(err)
			}

			bhash, err := chain.Load_Merkle_Hash(version)
			if err != nil {
				panic(err)
			}

			fmt.Printf("BALANCE_TREE : %s\n", bhash)
			fmt.Printf("MINING REWARD : %s\n", globals.FormatMoney(blockchain.CalcBlockReward(bl.Height)))

			//fmt.Printf("Orphan: %v\n",chain.Is_Block_Orphan(hash))

			//json_bytes, err := json.Marshal(bl)

			//fmt.Printf("%s  err : %s\n", string(prettyprint_json(json_bytes)), err)

		// can be used to debug/deserialize blocks
		// it can be used for blocks not in chain
		case command == "parse_block":

			if len(line_parts) != 2 {
				logger.Info("parse_block needs a block in hex format")
				continue
			}

			block_raw, err := hex.DecodeString(strings.ToLower(line_parts[1]))
			if err != nil {
				fmt.Printf("err while hex decoding block err %s\n", err)
				continue
			}

			var bl block.Block
			err = bl.Deserialize(block_raw)
			if err != nil {
				logger.Error(err, "Error deserializing block")
				continue
			}

			// decode and print block as much as possible
			fmt.Printf("%s\n", bl.String())
			fmt.Printf("Height: %d\n", bl.Height)
			tips_found := true
			for i := range bl.Tips {
				_, err := chain.Load_BL_FROM_ID(bl.Tips[i])
				if err != nil {
					fmt.Printf("Tips %s not in our DB", bl.Tips[i])
					tips_found = false
					continue
				}
			}

			expected_difficulty := new(big.Int).SetUint64(0)
			if tips_found { // we can solve diffculty
				expected_difficulty = chain.Get_Difficulty_At_Tips(bl.Tips)
				fmt.Printf("Difficulty:  %s\n", expected_difficulty.String())
			} else { // difficulty cann not solved

			}

		case command == "print_tx":
			if len(line_parts) == 2 && len(line_parts[1]) == 64 {
				txid, err := hex.DecodeString(strings.ToLower(line_parts[1]))

				if err != nil {
					fmt.Printf("err while decoding txid err %s\n", err)
					continue
				}
				var hash crypto.Hash
				copy(hash[:32], []byte(txid))

				var tx transaction.Transaction
				if tx_bytes, err := chain.Store.Block_tx_store.ReadTX(hash); err != nil {
					fmt.Printf("err while reading txid err %s\n", err)
					continue
				} else if err = tx.Deserialize(tx_bytes); err != nil {
					fmt.Printf("err deserializing tx err %s\n", err)
					continue
				}

				if valid_blid, invalid, valid := chain.IS_TX_Valid(hash); valid {
					fmt.Printf("TX is valid in block %s\n", valid_blid)
				} else if len(invalid) == 0 {
					fmt.Printf("TX is mined in a side chain\n")
				} else {
					fmt.Printf("TX is mined in blocks %+v\n", invalid)
				}
				if tx.IsRegistration() {
					fmt.Printf("Registration TX validity could not be detected\n")
				}

			} else {
				fmt.Printf("print_tx  needs a single transaction id as arugument\n")
			}

		case strings.ToLower(line) == "status":
			inc, out := p2p.Peer_Direction_Count()

			mempool_tx_count := len(chain.Mempool.Mempool_List_TX())
			regpool_tx_count := len(chain.Regpool.Regpool_List_TX())

			supply := uint64(0)

			supply = (config.PREMINE + blockchain.CalcBlockReward(uint64(chain.Get_Height()))*uint64(chain.Get_Height())) // valid for few years

			green := "\033[32m"      // default is green color
			yellow := "\033[33m"     // make prompt yellow
			red := "\033[31m"        // make prompt red
			blue := "\033[34m"       // blue color
			reset_color := "\033[0m" // reset color

			hostname, _ := os.Hostname()
			fmt.Printf(blue+"STATUS MENU for DERO HE Node (%s%s%s)\n\n", red, hostname, blue)

			threads := fmt.Sprintf("%sThreads %s[%s%d%s/%s%d%s]%s", yellow, blue, green, threadStartCount, blue, green, globals.CountThreads(), blue, reset_color)

			mutex := fmt.Sprintf("%sMutex %s[%s%d%s/%s%d%s]%s", yellow, blue, green, mutexStartCount, blue, green, globals.CountMutex(), blue, reset_color)

			blocked := fmt.Sprintf("%sBlocked %s[%s%d%s/%s%d%s]%s", yellow, blue, green, blockingStartCount, blue, green, globals.CountBlocked(), blue, reset_color)
			goprocs := fmt.Sprintf("%sGO Procs %s[%s%d%s/%s%d%s]%s", yellow, blue, green, goStartCount, blue, green, globals.CountGoProcs(), blue, reset_color)

			fmt.Printf(blue+"DERO 🥷 Daemon - %s - %s - %s - %s\n", threads, mutex, blocked, goprocs)

			fmt.Printf(blue+"Version: %s%s\n\n", yellow, config.Version.String())

			fmt.Printf(blue+"Hostname: %s%s %sUptime: %s%s %sBlock(s): %s%d\n", green, hostname, blue, green, time.Now().Sub(globals.StartTime).Round(time.Second).String(), blue, green, (chain.Get_Height() - globals.BlockChainStartHeight))
			fmt.Printf(blue+"Uptime Since: %s%s %sBlock: %s%d\n\n", green, globals.StartTime.Format(time.RFC1123), blue, green, globals.BlockChainStartHeight)

			fmt.Printf(blue+"Network "+red+"%s"+blue+" Height "+green+"%d"+blue+"  NW Hashrate "+green+"%0.03f MH/sec"+blue+"  Peers "+yellow+"%d"+blue+" inc, "+yellow+"%d"+blue+" out  MEMPOOL size "+yellow+"%d"+blue+" REGPOOL "+yellow+"%d"+blue+"  Total Supply "+yellow+"%s"+blue+" DERO \n", globals.Config.Name, chain.Get_Height(), float64(chain.Get_Network_HashRate())/1000000.0, inc, out, mempool_tx_count, regpool_tx_count, globals.FormatMoney(supply))

			tips := chain.Get_TIPS()
			fmt.Printf(blue + "Tips " + reset_color)
			for _, tip := range tips {
				fmt.Printf(" %s(%d)\n", tip, chain.Load_Height_for_BL_ID(tip))
			}

			if chain.LocatePruneTopo() >= 1 {
				fmt.Printf(blue+"\nChain is pruned till "+yellow+"%d"+reset_color+"\n", chain.LocatePruneTopo())
			} else {
				fmt.Printf("\nChain is in full mode.\n")
			}
			fmt.Printf(blue+"Integrator address "+red+"%s"+reset_color+"\n\n", chain.IntegratorAddress().String())
			fmt.Printf("UTC time %s  (as per system clock) \n", time.Now().UTC())
			fmt.Printf("UTC time %s  (offset %s) (as per daemon) should be close to 0\n", globals.Time().UTC(), time.Now().Sub(globals.Time()))
			fmt.Printf("Local time %s  (as per system clock) \n", time.Now())
			fmt.Printf("Local time %s  (offset %s) (as per daemon) should be close to 0\n", globals.Time(), time.Now().Sub(globals.Time()))
			fmt.Printf("Block Pop Count: %d\n\n", globals.BlockPopCount)

			total_orphans := p2p.CountNetworkOrphanSince(uint64(chain.Get_Height() - config.RunningConfig.NetworkStatsKeepCount))
			network_loss := float64(0)
			blockcount := config.RunningConfig.NetworkStatsKeepCount * 10
			if globals.CountTotalBlocks < blockcount {
				blockcount = globals.CountTotalBlocks
			} else {
				blockcount += int64(total_orphans)
			}
			if total_orphans > 0 && blockcount > 0 {
				network_loss = float64(float64(total_orphans)/float64(blockcount)) * 100
			}

			network_orphan_color := red
			if network_loss < 3 {
				network_orphan_color = yellow
			}
			if network_loss < 1.5 {
				network_orphan_color = green
			}

			fmt.Printf(blue+"Network Orphan Mini Block Rate: "+network_orphan_color+"%.2f%%"+reset_color+"\n", network_loss)

			fmt.Print(blue + "\nPeer Stats:\n" + reset_color)
			fmt.Printf("\tPeer ID:"+yellow+" %d\n"+reset_color, p2p.GetPeerID())
			fmt.Printf("\tNode Tag:"+yellow+" %s\n"+reset_color, p2p.GetNodeTag())

			blocksMinted := (globals.CountMinisAccepted + globals.CountBlocksAccepted)
			fmt.Print("\n" + blue + "Mining Stats:\n" + reset_color)
			fmt.Printf("\t"+blue+"Block Minted: "+green+"%d "+blue+"(MB+IB)"+reset_color+"\n", blocksMinted)
			if blocksMinted > 0 {

				velocity_1h := float64(float64(blocksMinted)/time.Now().Sub(globals.StartTime).Seconds()) * 3600
				if velocity_1h > float64(blocksMinted) {
					velocity_1h = float64(blocksMinted)
				}

				velocity_1d := float64(float64(blocksMinted)/time.Now().Sub(globals.StartTime).Seconds()) * 3600 * 24
				if velocity_1d > float64(blocksMinted) {
					velocity_1d = float64(blocksMinted)
				}

				fmt.Printf("\t"+blue+"Minting Velocity: "+green+"%.4f "+blue+"MB/h\t"+green+"%.4f "+blue+"MB/d (since uptime)"+reset_color+"\n", velocity_1h, velocity_1d)

			} else {
				fmt.Print("\t" + blue + "Minting Velocity: " + green + "0.0000 " + blue + "MB/h\t" + green + "0.0000 " + blue + "MB/d (since uptime)" + reset_color + "\n")
			}

			OrphanBlockRate := float64(0)
			my_orphan_blocks_count := globals.CountOrphanMinis + globals.CountOrphanBlocks
			if my_orphan_blocks_count > 0 {
				OrphanBlockRate = float64(float64(float64(my_orphan_blocks_count)/float64(blocksMinted)) * 100)
			}

			orphan_color := red
			if OrphanBlockRate < network_loss {
				orphan_color = yellow
			}
			if OrphanBlockRate <= 1 {
				orphan_color = green
			}
			fmt.Printf("\t"+blue+"My Orphan Block Rate:  "+orphan_color+"%.2f%% "+reset_color+"\n", OrphanBlockRate)

			ibo_color := green
			mbo_color := green
			mbr_color := green

			if globals.CountOrphanBlocks >= 1 {
				ibo_color = red
			}
			if globals.CountOrphanMinis >= 1 {
				mbo_color = red
			}
			if globals.CountMinisRejected >= 1 {
				mbr_color = red
			}

			fmt.Printf("\t"+blue+"IB:"+green+"%d "+blue+"MB:"+green+"%d "+blue+"IBO:"+ibo_color+"%d "+blue+"MBO:"+mbo_color+"%d "+blue+"MBR:"+mbr_color+"%d"+reset_color+"\n", globals.CountBlocksAccepted, globals.CountMinisAccepted, globals.CountOrphanBlocks, globals.CountOrphanMinis, globals.CountMinisRejected)
			fmt.Printf("\t"+blue+"MB "+green+"%.02f%%"+blue+"(1hr)\t"+green+"%.05f%%"+blue+"(1d)\t"+green+"%.06f%%"+blue+"(7d)\t(Moving average %%, will be 0 if no miniblock found)"+reset_color+"\n", derodrpc.HashrateEstimatePercent_1hr(), derodrpc.HashrateEstimatePercent_1day(), derodrpc.HashrateEstimatePercent_7day())
			mh_1hr := uint64((float64(chain.Get_Network_HashRate()) * derodrpc.HashrateEstimatePercent_1hr()) / 100)
			mh_1d := uint64((float64(chain.Get_Network_HashRate()) * derodrpc.HashrateEstimatePercent_1day()) / 100)
			mh_7d := uint64((float64(chain.Get_Network_HashRate()) * derodrpc.HashrateEstimatePercent_7day()) / 100)
			fmt.Printf("\t"+blue+"Avg Mining HR "+green+"%s"+blue+"(1hr)\t"+green+"%s"+blue+"(1d)\t"+green+"%s"+blue+"(7d)"+reset_color+"\n", hashratetostring(mh_1hr), hashratetostring(mh_1d), hashratetostring(mh_7d))
			fmt.Printf("\t"+blue+"Reward Generated (since uptime): "+green+"%s DERO\n"+reset_color, globals.FormatMoney(((blockchain.CalcBlockReward(uint64(chain.Get_Height())) / 10) * uint64(blocksMinted-my_orphan_blocks_count))))

			fmt.Printf("\n")
			fmt.Printf(blue+"Current Block Reward: "+yellow+"%s\n"+reset_color, globals.FormatMoney(blockchain.CalcBlockReward(uint64(chain.Get_Height()))))
			fmt.Printf("\n")

			// print hardfork status on second line
			hf_state, _, _, threshold, version, votes, window := chain.Get_HF_info()
			switch hf_state {
			case 0: // voting in progress
				locked := false
				if window == 0 {
					window = 1
				}
				if votes >= (threshold*100)/window {
					locked = true
				}
				fmt.Printf("Hard-Fork v%d in-progress need %d/%d votes to lock in, votes: %d, LOCKED:%+v\n", version, ((threshold * 100) / window), window, votes, locked)
			case 1: // daemon is old and needs updation
				fmt.Printf("Please update this daemon to  support Hard-Fork v%d\n", version)
			case 2: // everything is okay
				fmt.Printf("Hard-Fork v%d\n", version)

			}

		case command == "debug":

			log_level := config.RunningConfig.LogLevel
			if len(line_parts) == 2 {

				i, err := strconv.ParseInt(line_parts[1], 10, 64)
				if err != nil {
					io.WriteString(l.Stderr(), "usage: debug <level>\n")
				} else {
					log_level = int8(i)
				}

			} else {
				if config.RunningConfig.LogLevel > 0 {
					log_level = 0
				} else {
					log_level = 1
				}
			}

			ToggleDebug(l, log_level)

		case command == "uptime":

			hostname, _ := os.Hostname()

			fmt.Printf(blue+"Hostname: %s%s %sUptime: %s%s %sBlock(s): %s%d\n", green, hostname, blue, green, time.Now().Sub(globals.StartTime).Round(time.Second).String(), blue, green, (chain.Get_Height() - globals.BlockChainStartHeight))
			fmt.Printf(blue+"Uptime Since: %s%s %sBlock: %s%d\n\n", green, globals.StartTime.Format(time.RFC1123), blue, green, globals.BlockChainStartHeight)

		case command == "ban_above_height":

			if len(line_parts) == 2 {
				height := chain.Get_Height() + 100
				i, err := strconv.ParseInt(line_parts[1], 10, 64)
				if err != nil {
					io.WriteString(l.Stderr(), "usage: ban_above_height <height>\n")
				} else {
					height = int64(i)
					p2p.Ban_Above_Height(height)
				}
			}
		case command == "address_to_name":

			if len(line_parts) == 2 {
				result, err := derodrpc.AddressToName(nil, rpc.AddressToName_Params{Address: line_parts[1], TopoHeight: -1})

				if err == nil {
					fmt.Printf("\nAddress: %s has following names:\n", line_parts[1])
					for _, name := range result.Names {
						fmt.Printf("\t%s\n", name)
					}
					fmt.Print("\n")
				} else {
					fmt.Printf("\nAddress: %s (%s)\n\n", line_parts[1], err.Error())
				}
			} else {
				fmt.Printf("usage: address_to_name <wallet address>\n")
			}

		case command == "active_nodes":

			active_nodes := p2p.GetActiveNodesFromHeight(chain.Get_Height() - config.RunningConfig.NetworkStatsKeepCount)

			var ordered_nodes []string

			for node, _ := range active_nodes {

				ordered_nodes = append(ordered_nodes, node)
			}

			sort.SliceStable(ordered_nodes, func(i, j int) bool {
				return active_nodes[ordered_nodes[i]]["total"] > active_nodes[ordered_nodes[j]]["total"]
			})

			show_count := 25
			if len(line_parts) == 2 {

				i, err := strconv.ParseInt(line_parts[1], 10, 64)
				if err != nil {
					io.WriteString(l.Stderr(), "usage: active_nodes <show count - default 25>\n")
				} else {
					show_count = int(i)
				}
			}

			if show_count > len(active_nodes) {
				show_count = len(active_nodes)
			}

			height := chain.Get_Height()
			keep_blocks := config.RunningConfig.NetworkStatsKeepCount
			keep_string := fmt.Sprintf("Last %d Blocks", config.RunningConfig.NetworkStatsKeepCount)
			if (height - globals.BlockChainStartHeight) < config.RunningConfig.NetworkStatsKeepCount {
				keep_blocks = height - globals.BlockChainStartHeight
				keep_string = fmt.Sprintf("Last %d/%d Blocks", keep_blocks, config.RunningConfig.NetworkStatsKeepCount)
			}
			fmt.Printf("Network Mining Node Stats - %s - Showing %d/%d nodes\n\n", keep_string, show_count, len(active_nodes))
			fmt.Printf("%-30s %-8s %-8s %-8s %-14s %-16s\n", "Node IP", "IB", "MB", "MBO", "Orphan Loss", "Dominance")

			ib, mb := p2p.GetBlockLogLenght()

			total_blocks := float64(ib + mb)
			count := 0
			for _, node := range ordered_nodes {
				if count >= show_count {
					break
				}

				node_total_blocks := float64(active_nodes[node]["finals"] + active_nodes[node]["minis"])
				dominance := fmt.Sprintf("%.2f%%", ((node_total_blocks / total_blocks) * 100))
				orphan_loss := fmt.Sprintf("%.2f%%", (float64(active_nodes[node]["orphans"]) / float64(active_nodes[node]["total"]) * 100))
				fmt.Printf("%-30s %-8d %-8d %-8d %-14s %-16s\n", node, active_nodes[node]["finals"], active_nodes[node]["minis"], active_nodes[node]["orphans"], orphan_loss, dominance)
				count++
			}

			fmt.Printf("\nTotal Active Miner Node(s): %d\n", len(active_nodes))

		case command == "active_miners":

			active_miners := p2p.GetActiveMinersFromHeight(chain.Get_Height() - config.RunningConfig.NetworkStatsKeepCount)

			var ordered_minder []string

			for node, _ := range active_miners {

				ordered_minder = append(ordered_minder, node)
			}

			sort.SliceStable(ordered_minder, func(i, j int) bool {
				return active_miners[ordered_minder[i]]["total"] > active_miners[ordered_minder[j]]["total"]
			})

			show_count := 25
			if len(line_parts) == 2 {

				i, err := strconv.ParseInt(line_parts[1], 10, 64)
				if err != nil {
					io.WriteString(l.Stderr(), "usage: active_miners <show count - default 25>\n")
				} else {
					show_count = int(i)
				}
			}

			if show_count > len(active_miners) {
				show_count = len(active_miners)
			}

			height := chain.Get_Height()
			keep_blocks := config.RunningConfig.NetworkStatsKeepCount
			keep_string := fmt.Sprintf("Last %d Blocks", config.RunningConfig.NetworkStatsKeepCount)
			if (height - globals.BlockChainStartHeight) < config.RunningConfig.NetworkStatsKeepCount {
				keep_blocks = height - globals.BlockChainStartHeight
				keep_string = fmt.Sprintf("Last %d/%d Blocks", keep_blocks, config.RunningConfig.NetworkStatsKeepCount)
			}

			fmt.Printf("Network Mining Stats - %s - Showing %d/%d miners\n\n", keep_string, show_count, len(active_miners))

			fmt.Printf("%-76s %-8s %-8s %-8s %-8s %-14s %-16s %-26s\n", "Miner Address", "IB", "MB", "IBO", "MBO", "Orphan Loss", "Dominance", "Node (Probability)")

			var count int = 0
			for _, miner := range ordered_minder {
				if count >= show_count {
					break
				}

				dominance := fmt.Sprintf("%.02f%%", (float64(active_miners[miner]["total"]) / (10 * float64(keep_blocks)) * 100))
				node, probabiliy := p2p.BestGuessMinerNodeHeight((chain.Get_Height() - config.RunningConfig.NetworkStatsKeepCount), miner)

				orphan_loss := float64(float64(active_miners[miner]["orphans"]) / float64(active_miners[miner]["total"]) * 100)

				node_string := fmt.Sprintf("%-16s (%.2f%%)", node, probabiliy)
				orphan_string := fmt.Sprintf("%.2f%%", orphan_loss)
				fmt.Printf("%-76s %-8d %-8d %-8d %-8d %-14s %-16s %-26s\n", miner, active_miners[miner]["finals"], active_miners[miner]["minis"], active_miners[miner]["ibo"], active_miners[miner]["mbo"], orphan_string, dominance, node_string)
				count++

			}

			fmt.Printf("\nTotal Active Miner(s): %d\n", len(active_miners))

		case command == "connect_to_peer":

			if len(line_parts) == 2 {
				logger.Info(fmt.Sprintf("Connecting to: %s", line_parts[1]))

				address := line_parts[1]
				p2p.ConnecToNode(address)
			} else {
				fmt.Printf("usage: connect_to_peer <ip address:port>\n")
			}

		case command == "disconnect_peer":

			if len(line_parts) == 2 {
				address := line_parts[1]
				p2p.DisconnectAddress(address)
			} else {
				fmt.Printf("usage: disconnect_peer <ip address>\n")
			}

		case command == "miner_info":

			if len(line_parts) == 2 {

				active_miners := p2p.GetActiveMinersFromHeight(chain.Get_Height() - config.RunningConfig.NetworkStatsKeepCount)
				height := chain.Get_Height()
				keep_blocks := config.RunningConfig.NetworkStatsKeepCount
				keep_string := fmt.Sprintf("Last %d Blocks", config.RunningConfig.NetworkStatsKeepCount)
				if (height - globals.BlockChainStartHeight) < config.RunningConfig.NetworkStatsKeepCount {
					keep_blocks = height - globals.BlockChainStartHeight
					keep_string = fmt.Sprintf("Last %d/%d Blocks", keep_blocks, config.RunningConfig.NetworkStatsKeepCount)
				}
				fmt.Printf("Network Mining Stats Since %s\n\n", keep_string)
				for miner, _ := range active_miners {
					if miner != line_parts[1] {
						continue
					}

					fmt.Printf("%-76s %-16s %-16s %-16s %-24s\n", "Miner", "IB", "MB", "MBO", "Dominance")
					dominance := fmt.Sprintf("%.02f%%", (float64(active_miners[miner]["total"]) / (10 * float64(keep_blocks)) * 100))

					orphan_loss := float64(float64(active_miners[miner]["orphans"]) / float64(active_miners[miner]["total"]) * 100)
					orphan_string := fmt.Sprintf("%.2f%%", orphan_loss)

					fmt.Printf("%-76s %-16d %-16d %-16d %-24s\n", miner, active_miners[miner]["finals"], active_miners[miner]["minis"], active_miners[miner]["orphans"], dominance)

					ordered_nodes, data := p2p.PotentialMinerNodeHeight((chain.Get_Height() - 100), miner)
					fmt.Print("\nPotential Miner Nodes:\n")
					fmt.Printf("%-24s %-8s %-8s %-8s %-14s %-16s\n", "Node", "IB", "MB", "MBO", "Orphan Loss", "Probability")

					for _, node := range ordered_nodes {
						fmt.Printf("%-24s %-8d %-8d %-8d %-14s %.2f%%\n", node, int(data[node]["finals"]), int(data[node]["minis"]), int(data[node]["orphans"]), orphan_string, data[node]["likelyhood"])
					}

				}
				fmt.Print("\n")
				// derodrpc.ShowMinerInfo(line_parts[1])
			} else {
				fmt.Printf("usage: miner_info <wallet address/ip>\n")
			}

		case command == "list_miners":

			derodrpc.ListMiners()

		case command == "mined_blocks":

			fmt.Print("Mined Blocks List\n\n")

			fmt.Printf("%-72s %-12s %s\n\n", "Wallet", "Height", "Block")

			blocks := p2p.GetMyBlocksCollection()
			count := 0
			for miner, block_list := range blocks {

				// wallet := derodrpc.GetMinerWallet(miner)

				for _, mbl := range block_list {
					hash, err := chain.Load_Block_Topological_order_at_index(int64(mbl.Height))
					if err != nil {
						fmt.Printf("Skipping block at topo height %d - likely not committed yet\n", mbl.Height)

					} else {

						fmt.Printf("%-72s %-12d %s\n", miner, mbl.Height, hash)
					}
					count++
				}

			}

			fmt.Printf("Mined Blocks Collection Size: %d\n", count)
			fmt.Print("\n")

		case command == "peer_info":

			var error_peer string

			if len(line_parts) == 2 {
				error_peer = p2p.ParseIPNoError(line_parts[1])
				p2p.Print_Peer_Info(error_peer)

				integrators, integrator_data := p2p.PotentialNodeIntegratorsFromHeight(chain.Get_Height()-config.RunningConfig.NetworkStatsKeepCount, error_peer)

				height := chain.Get_Height()
				keep_blocks := config.RunningConfig.NetworkStatsKeepCount
				keep_string := fmt.Sprintf("Last %d Blocks", config.RunningConfig.NetworkStatsKeepCount)
				if (height - globals.BlockChainStartHeight) < config.RunningConfig.NetworkStatsKeepCount {
					keep_blocks = height - globals.BlockChainStartHeight
					keep_string = fmt.Sprintf("Last %d/%d Blocks", keep_blocks, config.RunningConfig.NetworkStatsKeepCount)
				}
				fmt.Printf("\nPotential Integrators - %s\n\n", keep_string)

				fmt.Printf("%-76s %-16s %-24s\n", "Miner Address", "IB", "% of Total")

				for _, miner := range integrators {

					fmt.Printf("%-76s %-16d %0.2f%%\n", miner, int(integrator_data[miner]["finals"]), integrator_data[miner]["likelyhood"])

				}

				ordered_miner, data := p2p.PotentialMinersOnNodeFromHeight(chain.Get_Height()-config.RunningConfig.NetworkStatsKeepCount, error_peer)

				fmt.Printf("\nPotential Miners - %s\n\n", keep_string)

				fmt.Printf("%-76s %-16s %-16s %-16s %-24s\n", "Miner Address", "IB", "MB", "MBO", "% of Total")

				for _, miner := range ordered_miner {

					fmt.Printf("%-76s %-16d %-16d %-16d %0.2f%%\n", miner, int(data[miner]["finals"]), int(data[miner]["minis"]), int(data[miner]["orphans"]), data[miner]["likelyhood"])

				}
				fmt.Print("\n")
			} else {
				fmt.Printf("usage: peer_info <ip address>\n")
			}

		case command == "run_diagnostics":

			if globals.DiagnocticCheckRunning {
				fmt.Printf("ERR: Diagnostic Checking already running ...\n")
			} else {
				globals.NextDiagnocticCheck = time.Now().Unix() - 1
				go RunDiagnosticCheckSquence(chain, l)
			}

		case command == "config":

			if len(line_parts) >= 2 {
				if line_parts[1] == "p2p_bwfactor" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "bw factor need to be number\n")
					} else {
						config.RunningConfig.P2PBWFactor = i
					}
				}
				if line_parts[1] == "min_peers" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "min peers need to be number\n")
					} else {
						p2p.Min_Peers = i
						if p2p.Max_Peers < p2p.Min_Peers {
							p2p.Max_Peers = p2p.Min_Peers
							config.RunningConfig.Max_Peers = p2p.Max_Peers
						}
						config.RunningConfig.Min_Peers = p2p.Min_Peers

					}
				}
				if line_parts[1] == "max_peers" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "max peers need to be number\n")
					} else {
						p2p.Max_Peers = i
						if p2p.Min_Peers > p2p.Max_Peers {
							p2p.Min_Peers = p2p.Max_Peers
							config.RunningConfig.Min_Peers = p2p.Min_Peers
						}
						config.RunningConfig.Max_Peers = p2p.Max_Peers
					}
				}

				if line_parts[1] == "peer_log_expiry" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "peer_log_expiry time need to be in seconds\n")
					} else {
						config.RunningConfig.ErrorLogExpirySeconds = i

					}
				}

				if line_parts[1] == "network_stats_keep" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "network_stats_keep <amount of blocks to keep>\n")
					} else {
						config.RunningConfig.NetworkStatsKeepCount = i

					}
				}

				if line_parts[1] == "diagnostic_delay" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "diagnostic_delay in seconds\n")
					} else {
						config.RunningConfig.DiagnosticCheckDelay = i

					}
				}

				if line_parts[1] == "block_reject_threshold" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "block_reject_threshold in seconds\n")
					} else {
						config.RunningConfig.BlockRejectThreshold = i

					}
				}

				if line_parts[1] == "peer_latency_threshold" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "peer_latency_threshold in seconds\n")
					} else {
						config.RunningConfig.PeerLatencyThreshold = time.Duration(i * int64(time.Millisecond))
					}
				}

				if line_parts[1] == "job_dispatch_time" && len(line_parts) == 3 {
					i, err := strconv.ParseInt(line_parts[2], 10, 64)
					if err != nil {
						io.WriteString(l.Stderr(), "dispatch time need to be in miliseconds\n")
					} else {
						config.RunningConfig.GETWorkJobDispatchTime = time.Duration(i * int64(time.Millisecond))

					}
				}

				if line_parts[1] == "block_tracking" {
					if config.RunningConfig.TraceBlocks {
						config.RunningConfig.TraceBlocks = false
					} else {
						config.RunningConfig.TraceBlocks = true
					}
				}

				if line_parts[1] == "tx_tracking" {
					if config.RunningConfig.TraceTx {
						config.RunningConfig.TraceTx = false
					} else {
						config.RunningConfig.TraceTx = true
					}
				}

				if line_parts[1] == "trusted" {
					if config.RunningConfig.OnlyTrusted {
						config.RunningConfig.OnlyTrusted = false
					} else {
						config.RunningConfig.OnlyTrusted = true
						p2p.Only_Trusted_Peers()
					}
				}

				if line_parts[1] == "track_tagged" {
					if config.RunningConfig.TraceTagged {
						config.RunningConfig.TraceTagged = false
					} else {
						config.RunningConfig.TraceTagged = true
					}
				}

				if line_parts[1] == "p2p_turbo" {
					if config.RunningConfig.P2PTurbo {
						config.RunningConfig.P2PTurbo = false
					} else {
						config.RunningConfig.P2PTurbo = true
					}
				}

				if line_parts[1] == "variable_dispatch" {
					if config.RunningConfig.VariableDispatchTime {
						config.RunningConfig.VariableDispatchTime = false
					} else {
						config.RunningConfig.VariableDispatchTime = true
					}
				}

				if line_parts[1] == "maintenance" {
					if len(line_parts) == 3 {
						config.RunningConfig.MinerMaintenanceMessage = line_parts[2]
					} else {
						globals.NodeMaintenance = true
						globals.MaintenanceStart = time.Now().Unix()
					}
				}

				if line_parts[1] == "whitelist_incoming" {

					if config.RunningConfig.WhitelistIncoming {
						config.RunningConfig.WhitelistIncoming = false
					} else {
						config.RunningConfig.WhitelistIncoming = true
					}
				}
				if line_parts[1] == "operator" && len(line_parts) == 3 {
					config.RunningConfig.OperatorName = line_parts[2]
				}
				if line_parts[1] == "node_tag" {
					new_tag := ""
					for i := 2; i < len(line_parts); i++ {
						new_tag = fmt.Sprintf("%s %s", new_tag, line_parts[i])
					}

					new_tag = strings.TrimLeft(new_tag, " ")
					new_tag = strings.TrimRight(new_tag, " ")
					p2p.SetNodeTag(new_tag)
				}
				if line_parts[1] == "anti_cheat" && len(line_parts) == 2 {

					if config.RunningConfig.AntiCheat {
						config.RunningConfig.AntiCheat = false
						io.WriteString(l.Stderr(), "anti_cheat disabled\n")

					} else {
						config.RunningConfig.AntiCheat = true
						io.WriteString(l.Stderr(), "anti_cheat enabled\n")
					}
				}
				save_config_file()
			}

			io.WriteString(l.Stdout(), blue+" Config Menu\n\n"+reset_color)
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-24s %-24s\n\n", blue+"Option", red+"Value", yellow+"How to change"+reset_color))

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Operator Name", config.RunningConfig.OperatorName, "config operator <name>"))

			whitelist_incoming := "YES"
			if !config.RunningConfig.WhitelistIncoming {
				whitelist_incoming = "NO"
			}
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Whitelist Incoming Peers", whitelist_incoming, "config whitelist_incoming"))

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "P2P Node Tag", p2p.GetNodeTag(), "config node_tag <Tag or none to remove>"))

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20d %-20s\n", "P2P Min Peers", p2p.Min_Peers, "config min_peers <num>"))
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20d %-20s\n", "P2P Max Peers", p2p.Max_Peers, "config max_peers <num>"))

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Miner Maintenance Message", config.RunningConfig.MinerMaintenanceMessage, "config maitenance [new message] - will send message for 5 min or until restart"))

			blid, _ := chain.Load_Block_Topological_order_at_index(chain.Get_Height())
			blid50, _ := chain.Load_Block_Topological_order_at_index(chain.Get_Height() - 50)

			now := chain.Load_Block_Timestamp(blid)
			now50 := chain.Load_Block_Timestamp(blid50)
			AverageBlockTime50 := float32(now-now50) / (50.0 * 1000)
			seconds := AverageBlockTime50 * float32(config.RunningConfig.NetworkStatsKeepCount)
			blocksavetime := time.Duration(seconds * float32(time.Second)).Round(time.Millisecond)

			network_stats := fmt.Sprintf("%d (%s)", config.RunningConfig.NetworkStatsKeepCount, blocksavetime)

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Network Block To Keep for Stats", network_stats, "config network_stats_keep <blocks count>"))

			turbo := "OFF"
			if config.RunningConfig.P2PTurbo {
				turbo = "ON"
			}
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "P2P Turbo", turbo, "config p2p_turbo"))
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20d %-20s\n", "P2P BW Factor", config.RunningConfig.P2PBWFactor, "config p2p_bwfactor <num>"))

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "GETWORK - Job will be dispatch time", config.RunningConfig.GETWorkJobDispatchTime, "config job_dispatch_time <miliseconds>"))
			variable_dispatch := "OFF"
			if config.RunningConfig.VariableDispatchTime {
				variable_dispatch = "ON"
			}
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "GETWORK - Variable Dispatch Time", variable_dispatch, "config variable_dispatch"))

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20d %-20s\n", "Peer Log Expiry (sec)", config.RunningConfig.ErrorLogExpirySeconds, "config peer_log_expiry <seconds>"))

			trusted_only := "OFF"
			if config.RunningConfig.OnlyTrusted {
				trusted_only = "ON"
			}
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Connect to Trusted Only", trusted_only, "config trusted"))

			block_trace := "OFF"
			if config.RunningConfig.TraceBlocks {
				block_trace = "ON"
			}
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Track Live Blocks", block_trace, "config block_tracking"))
			tx_trace := "OFF"
			if config.RunningConfig.TraceTx {
				tx_trace = "ON"
			}
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Track Live TX", tx_trace, "config tx_tracking"))

			anto_cheat := "OFF"
			if config.RunningConfig.AntiCheat {
				anto_cheat = "ON"
			}
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Anti Cheat (Forced Mining Solo Fees)", anto_cheat, "config anti_cheat"))

			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20d %-20s\n", "Auto run diagnostic sequence every (seconds)", config.RunningConfig.DiagnosticCheckDelay, "config diagnostic_delay <seconds>"))

			io.WriteString(l.Stdout(), fmt.Sprintf("\n\tDiagnostic Thresholds - use (run_diagnostic) to test\n"))
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20d %-20s\n", "Block Transmission Success Rate Threshold", config.RunningConfig.BlockRejectThreshold, "config block_reject_threshold <seconds>"))
			io.WriteString(l.Stdout(), fmt.Sprintf("\t%-60s %-20s %-20s\n", "Peer Latency Threshold (Miliseconds)", time.Duration(config.RunningConfig.PeerLatencyThreshold).Round(time.Millisecond).String(), "config peer_latency_threshold <seconds>"))

			io.WriteString(l.Stdout(), "\n")

		case command == "add_trusted":

			var trusted string

			if len(line_parts) == 2 {
				trusted = line_parts[1]
				p2p.Add_Trusted(trusted)
			} else {
				fmt.Printf("usage: add_trusted <ip address>\n")
			}

		case command == "remove_trusted":

			var trusted string

			if len(line_parts) == 2 {
				trusted = line_parts[1]
				p2p.Del_Trusted(trusted)
			} else {
				fmt.Printf("usage: remove_trusted <ip address>\n")
			}

		case command == "show_selfish":

			p2p.Show_Selfish_Peers()

		case command == "list_trusted":

			p2p.Print_Trusted_Peers()

		case command == "peer_errors":

			var error_peer string

			if len(line_parts) == 2 {
				error_peer = line_parts[1]
				p2p.PrintPeerErrors(error_peer)
			} else {
				p2p.PrintBlockErrors()
			}

		case command == "clear_all_peer_stats":
			fmt.Print("Cleaing FailCount for all peers")
			go p2p.ClearAllStats()

		case command == "clear_peer_stats":

			var ip string

			if len(line_parts) == 2 {
				ip = line_parts[1]
				go p2p.ClearPeerStats(ip)
			} else {
				fmt.Printf("usage: clear_peer_stats <ip address>\n")
			}

		case command == "peer_list": // print peer list

			limit := int64(25)

			if len(line_parts) == 2 {
				if s, err := strconv.ParseInt(line_parts[1], 10, 64); err == nil && s >= 0 {
					limit = s
				}
			}

			p2p.PeerList_Print(limit)

		case strings.ToLower(line) == "syncinfo", strings.ToLower(line) == "sync_info": // print active connections
			p2p.Connection_Print()
		case strings.ToLower(line) == "bye":
			fallthrough
		case strings.ToLower(line) == "exit":
			close(Exit_In_Progress)

			return nil

		case strings.ToLower(line) == "quit":
			close(Exit_In_Progress)
			return nil

		case command == "graph":
			start := int64(0)
			stop := int64(0)

			if len(line_parts) != 3 {
				logger.Info("This function requires 2 parameters, start height and end height\n")
				continue
			}
			if s, err := strconv.ParseInt(line_parts[1], 10, 64); err == nil {
				start = s
			} else {
				logger.Error(err, "Invalid start value")
				continue
			}

			if s, err := strconv.ParseInt(line_parts[2], 10, 64); err == nil {
				stop = s
			} else {
				logger.Error(err, "Invalid stop value")
				continue
			}

			if start < 0 || start > int64(chain.Load_TOPO_HEIGHT()) {
				logger.Info("Start value should be be between 0 and current height")
				continue
			}
			if start > stop || stop > int64(chain.Load_TOPO_HEIGHT()) {
				logger.Info("Stop value should be > start and current height\n")
				continue
			}

			logger.Info("Writing block chain graph dot format  /tmp/graph.dot\n", "start", start, "stop", stop)
			WriteBlockChainTree(chain, "/tmp/graph.dot", start, stop)

		case command == "pop":
			switch len(line_parts) {
			case 1:
				chain.Rewind_Chain(1)
			case 2:
				pop_count := 0
				if s, err := strconv.Atoi(line_parts[1]); err == nil {
					pop_count = s

					if chain.Rewind_Chain(int(pop_count)) {
						logger.Info("Rewind successful")
					} else {
						logger.Error(fmt.Errorf("Rewind failed"), "")
					}

				} else {
					logger.Error(fmt.Errorf("POP needs argument n to pop this many blocks from the top"), "")
				}

			default:
				logger.Error(fmt.Errorf("POP needs argument n to pop this many blocks from the top"), "")
			}

		case command == "gc":
			runtime.GC()
		case command == "heap":
			if len(line_parts) == 1 {
				fmt.Printf("heap needs a filename to write\n")
				break
			}
			dump(line_parts[1])

		case command == "permban":

			if len(line_parts) >= 3 || len(line_parts) == 1 {
				fmt.Printf("IP address required to ban\n")
				break
			}

			err := p2p.PermBan_Address(line_parts[1]) // default ban is 10 minutes
			if err != nil {
				fmt.Printf("err parsing address %s", err)
				break
			}

		case command == "ban":

			if len(line_parts) >= 4 || len(line_parts) == 1 {
				fmt.Printf("IP address required to ban\n")
				break
			}

			if len(line_parts) == 3 { // process ban time if provided
				// if user provided a time, apply ban for specific time
				if s, err := strconv.ParseInt(line_parts[2], 10, 64); err == nil && s >= 0 {
					p2p.Ban_Address(line_parts[1], uint64(s))
					break
				} else {
					fmt.Printf("err parsing ban time (only positive number) %s", err)
					break
				}
			}

			err := p2p.Ban_Address(line_parts[1], 10*60) // default ban is 10 minutes
			if err != nil {
				fmt.Printf("err parsing address %s", err)
				break
			}

		case command == "unban":

			if len(line_parts) >= 3 || len(line_parts) == 1 {
				fmt.Printf("IP address required to unban\n")
				break
			}

			err := p2p.UnBan_Address(line_parts[1])
			if err != nil {
				fmt.Printf("err unbanning %s, err = %s", line_parts[1], err)
			} else {
				fmt.Printf("unbann %s successful", line_parts[1])
			}

		case command == "connect_to_seeds":
			for _, ip := range config.Mainnet_seed_nodes {
				logger.Info(fmt.Sprintf("Connecting to: %s", ip))
				go p2p.ConnecToNode(ip)
			}

		case command == "bans":
			p2p.BanList_Print() // print ban list

		case line == "sleep":
			logger.Info("console sleeping for 1 second")
			time.Sleep(1 * time.Second)
		case line == "":
		default:
			logger.Info(fmt.Sprintf("you said: %s", strconv.Quote(line)))
		}
	}

	return fmt.Errorf("can never reach here")

}

func writenode(chain *blockchain.Blockchain, w *bufio.Writer, blid crypto.Hash, start_height int64) { // process a node, recursively

	w.WriteString(fmt.Sprintf("node [ fontsize=12 style=filled ]\n{\n"))

	color := "white"

	if chain.Isblock_SideBlock(blid) {
		color = "yellow"
	}
	if chain.IsBlockSyncBlockHeight(blid) {
		color = "green"
	}

	// now dump the interconnections
	bl, err := chain.Load_BL_FROM_ID(blid)

	var acckey crypto.Point
	if err := acckey.DecodeCompressed(bl.Miner_TX.MinerAddress[:]); err != nil {
		panic(err)
	}

	addr := rpc.NewAddressFromKeys(&acckey)
	addr.Mainnet = globals.IsMainnet()

	w.WriteString(fmt.Sprintf("L%s  [ fillcolor=%s label = \"%s %d height %d score %d  order %d\nminer %s\"  ];\n", blid.String(), color, blid.String(), 0, chain.Load_Height_for_BL_ID(blid), 0, chain.Load_Block_Topological_order(blid), addr.String()))
	w.WriteString(fmt.Sprintf("}\n"))

	if err != nil {
		fmt.Printf("err loading block %s err %s\n", blid, err)
		return
	}
	if int64(bl.Height) > start_height {
		for i := range bl.Tips {
			w.WriteString(fmt.Sprintf("L%s -> L%s ;\n", bl.Tips[i].String(), blid.String()))
		}
	}

}

func hashratetostring(hash_rate uint64) string {
	hash_rate_string := ""

	switch {
	case hash_rate > 1000000000000:
		hash_rate_string = fmt.Sprintf("%.3f TH/s", float64(hash_rate)/1000000000000.0)
	case hash_rate > 1000000000:
		hash_rate_string = fmt.Sprintf("%.3f GH/s", float64(hash_rate)/1000000000.0)
	case hash_rate > 1000000:
		hash_rate_string = fmt.Sprintf("%.3f MH/s", float64(hash_rate)/1000000.0)
	case hash_rate > 1000:
		hash_rate_string = fmt.Sprintf("%.3f KH/s", float64(hash_rate)/1000.0)
	case hash_rate > 0:
		hash_rate_string = fmt.Sprintf("%d H/s", hash_rate)
	}
	return hash_rate_string
}

func WriteBlockChainTree(chain *blockchain.Blockchain, filename string, start_height, stop_height int64) (err error) {

	var node_map = map[crypto.Hash]bool{}

	for i := start_height; i < stop_height; i++ {
		blids := chain.Get_Blocks_At_Height(i)

		for _, blid := range blids {
			if _, ok := node_map[blid]; ok {
				panic("duplicate block should not be there")
			} else {
				node_map[blid] = true
			}
		}
	}

	f, err := os.Create(filename)
	if err != nil {
		return
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()
	w.WriteString("digraph dero_blockchain_graph { \n")

	for blid := range node_map {
		writenode(chain, w, blid, start_height)
	}
	//g := Generate_Genesis_Block()
	//writenode(chain, dbtx, w, g.GetHash())

	w.WriteString("}\n")

	return
}

func prettyprint_json(b []byte) []byte {
	var out bytes.Buffer
	err := json.Indent(&out, b, "", "  ")
	_ = err
	return out.Bytes()
}

func usage(w io.Writer) {
	io.WriteString(w, "commands:\n")
	io.WriteString(w, "\t\033[1mhelp\033[0m\t\tthis help\n")
	io.WriteString(w, "\t\033[1mdiff\033[0m\t\tShow difficulty\n")
	io.WriteString(w, "\t\033[1mprint_bc\033[0m\tPrint blockchain info in a given blocks range, print_bc <begin_height> <end_height>\n")
	io.WriteString(w, "\t\033[1mprint_block\033[0m\tPrint block, print_block <block_hash> or <block_height>\n")
	io.WriteString(w, "\t\033[1mprint_tx\033[0m\tPrint transaction, print_tx <transaction_hash>\n")
	io.WriteString(w, "\t\033[1mstatus\033[0m\t\tShow general information\n")
	io.WriteString(w, "\t\033[1mpeer_list\033[0m\tPrint peer list\n")
	io.WriteString(w, "\t\033[1msyncinfo\033[0m\tPrint information about connected peers and their state\n")

	io.WriteString(w, "\t\033[1mbye\033[0m\t\tQuit the daemon\n")
	io.WriteString(w, "\t\033[1mban\033[0m\t\tBan specific ip from making any connections\n")
	io.WriteString(w, "\t\033[1munban\033[0m\t\tRevoke restrictions on previously banned ips\n")
	io.WriteString(w, "\t\033[1mbans\033[0m\t\tPrint current ban list\n")
	io.WriteString(w, "\t\033[1mmempool_print\033[0m\t\tprint mempool contents\n")
	io.WriteString(w, "\t\033[1mmempool_delete_tx\033[0m\t\tDelete specific tx from mempool\n")
	io.WriteString(w, "\t\033[1mmempool_flush\033[0m\t\tFlush regpool\n")
	io.WriteString(w, "\t\033[1mregpool_print\033[0m\t\tprint regpool contents\n")
	io.WriteString(w, "\t\033[1mregpool_delete_tx\033[0m\t\tDelete specific tx from regpool\n")
	io.WriteString(w, "\t\033[1mregpool_flush\033[0m\t\tFlush mempool\n")
	io.WriteString(w, "\t\033[1msetintegratoraddress\033[0m\t\tChange current integrated address\n")
	io.WriteString(w, "\t\033[1mversion\033[0m\t\tShow version\n")
	io.WriteString(w, "\t\033[1mexit\033[0m\t\tQuit the daemon\n")
	io.WriteString(w, "\t\033[1mquit\033[0m\t\tQuit the daemon\n")

	io.WriteString(w, "\n\nHansen33-Mod commands:\n")

	io.WriteString(w, "\t\033[1mpermban <ip>\033[0m\t\tPermanent ban IP - make sure IP stays banned until unban\n")
	io.WriteString(w, "\t\033[1mconfig\033[0m\t\tSee and set running config options\n")
	io.WriteString(w, "\t\033[1mrun_diagnostics\033[0m\t\tRun Diagnostics Checks\n")
	io.WriteString(w, "\t\033[1muptime\033[0m\t\tDisplay Daemon Uptime Info\n")
	io.WriteString(w, "\t\033[1mdebug\033[0m\t\tToggle debug ON/OFF\n")
	io.WriteString(w, "\t\033[1mpeer_list (modified)\033[0m\tPrint peer list\n")
	io.WriteString(w, "\t\033[1msyncinfo (modified)\033[0m\tPrint more peer list\n")
	io.WriteString(w, "\t\033[1mpeer_info\033[0m\tPrint peer information. To see details use - peer_info <ip>\n")
	io.WriteString(w, "\t\033[1mpeer_errors\033[0m\tPrint peer errors. To see details use - peer_errors <ip>\n")
	io.WriteString(w, "\t\033[1mclear_all_peer_stats\033[0m\tClear all peers stats\n")
	io.WriteString(w, "\t\033[1mclear_peer_stats\033[0m\tClear peer stats. To see details use - clear_peer_stats <ip>\n")
	io.WriteString(w, "\t\033[1mban_above_height\033[0m\tBan Peers fro 3600 seconds which has height above X - ban_above_height <height>\n")

	io.WriteString(w, "\t\033[1madd_trusted\033[0m\tTrusted Peer - add_trusted <ip/tag>\n")
	io.WriteString(w, "\t\033[1mremove_trusted\033[0m\tTrusted Peer - remove_trusted <ip/tag>\n")
	io.WriteString(w, "\t\033[1mlist_trusted\033[0m\tShow Trusted Peer List\n")
	io.WriteString(w, "\t\033[1mconnect_to_seeds\033[0m\tConnect to all seed nodes (see status in list_trusted)\n")
	io.WriteString(w, "\t\033[1mconnect_to_peer\033[0m\tConnect to any peer using - connect_to_peer <ip:p2p-port>\n")
	io.WriteString(w, "\t\033[1mdisconnect_peer\033[0m\tConnect to any peer using - disconnect_peer <ip>\n")
	io.WriteString(w, "\t\033[1mlist_miners\033[0m\tShow Connected Miners\n")
	io.WriteString(w, "\t\033[1mminer_info\033[0m\tDetailed miner info - miner_info <wallet>\n")
	io.WriteString(w, "\t\033[1mmined_blocks\033[0m\tList Mined Blocks\n")
	io.WriteString(w, "\t\033[1maddress_to_name\033[0m\tLookup registered names for Address\n")
	io.WriteString(w, "\t\033[1mactive_miners\033[0m\tShow Active Miners on Network\n")
	io.WriteString(w, "\t\033[1mactive_nodes\033[0m\tShow Active Mining Nodes\n")
	io.WriteString(w, "\t\033[1mshow_selfish\033[0m\tShow Nodes that don't play nice\n")

}

var completer = readline.NewPrefixCompleter(
	readline.PcItem("help"),
	readline.PcItem("diff"),
	readline.PcItem("gc"),
	readline.PcItem("mempool_dump"),
	readline.PcItem("mempool_flush"),
	readline.PcItem("mempool_delete_tx"),
	readline.PcItem("mempool_print"),
	readline.PcItem("regpool_flush"),
	readline.PcItem("regpool_delete_tx"),
	readline.PcItem("regpool_print"),
	readline.PcItem("peer_list"),
	readline.PcItem("print_bc"),
	readline.PcItem("print_block"),
	readline.PcItem("block_export"),
	readline.PcItem("block_import"),
	//	readline.PcItem("print_tx"),
	readline.PcItem("setintegratoraddress"),
	readline.PcItem("status"),
	readline.PcItem("syncinfo"),
	readline.PcItem("version"),
	readline.PcItem("bye"),
	readline.PcItem("exit"),
	readline.PcItem("quit"),

	readline.PcItem("uptime"),
	readline.PcItem("peer_errors"),
	readline.PcItem("clear_all_peer_stats"),
	readline.PcItem("clear_peer_stats"),
	readline.PcItem("peer_info"),
	readline.PcItem("debug"),
	readline.PcItem("run_diagnostics"),
	readline.PcItem("permban"),
	readline.PcItem("config"),
	readline.PcItem("ban_above_height"),
	readline.PcItem("add_trusted"),
	readline.PcItem("remove_trusted"),
	readline.PcItem("list_trusted"),
	readline.PcItem("connect_to_peer"),
	readline.PcItem("disconnect_peer"),
	readline.PcItem("connect_to_seeds"),
	readline.PcItem("list_miners"),
	readline.PcItem("active_miners"),
	readline.PcItem("active_nodes"),
	readline.PcItem("miner_info"),
	readline.PcItem("mined_blocks"),
	readline.PcItem("address_to_name"),
	readline.PcItem("show_selfish"),
)

func filterInput(r rune) (rune, bool) {
	switch r {
	// block CtrlZ feature
	case readline.CharCtrlZ:
		return r, false
	}
	return r, true
}

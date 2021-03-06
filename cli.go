package solar

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/SBit-Project/solar/contract"
	"github.com/SBit-Project/solar/deployer"
	"github.com/SBit-Project/solar/deployer/eth"
	"github.com/SBit-Project/solar/deployer/sbit"
	"github.com/SBit-Project/solar/varstr"
	"github.com/pkg/errors"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	app               = kingpin.New("solar", "Solidity smart contract deployment management.")
	sbitRPC           = app.Flag("sbit_rpc", "RPC provider url").Envar("SBIT_RPC").String()
	sbitSenderAddress = app.Flag("sbit_sender", "(sbit) Sender UTXO Address").Envar("SBIT_SENDER").String()

	// geth --rpc --rpcapi="eth,personal,miner"
	ethRPC    = app.Flag("eth_rpc", "RPC provider url").Envar("ETH_RPC").String()
	solarEnv  = app.Flag("env", "Environment name").Envar("SOLAR_ENV").Default("development").String()
	solarRepo = app.Flag("repo", "Path of contracts repository").Envar("SOLAR_REPO").String()
	appTasks  = map[string]func() error{}

	solcOptimize   = app.Flag("optimize", "[solc] should Enable bytecode optimizer").Default("true").Bool()
	solcAllowPaths = app.Flag("allow-paths", "[solc] Allow a given path for imports. A list of paths can be supplied by separating them with a comma.").Default("").String()
)

type RPCPlatform int

const (
	RPCSbit     = iota
	RPCEthereum = iota
)

type solarCLI struct {
	depoyer      deployer.Deployer
	deployerOnce sync.Once

	repo     *contract.ContractsRepository
	repoOnce sync.Once

	reporter     *events
	reporterOnce sync.Once
}

var solar = &solarCLI{}

var (
	errorUnspecifiedRPC = errors.New("Please specify RPC url by setting SBIT_RPC or ETH_RPC or using flag --sbit_rpc or --eth_rpc")
)

func (c *solarCLI) RPCPlatform() RPCPlatform {
	if *sbitRPC == "" && *ethRPC == "" {
		log.Fatalln(errorUnspecifiedRPC)
	}

	if *sbitRPC != "" {
		return RPCSbit
	}

	return RPCEthereum
}

func (c *solarCLI) Reporter() *events {
	c.reporterOnce.Do(func() {
		c.reporter = &events{
			in: make(chan interface{}),
		}

		go c.reporter.Start()
	})

	return c.reporter
}

func (c *solarCLI) SolcOptions() (*CompilerOptions, error) {
	allowPathsStr := *solcAllowPaths
	if allowPathsStr == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, errors.Wrap(err, "solc options")
		}

		allowPathsStr = cwd
	}

	allowPaths := strings.Split(allowPathsStr, ",")

	return &CompilerOptions{
		NoOptimize: !*solcOptimize,
		AllowPaths: allowPaths,
	}, nil
}

// Open the file `solar.{SOLAR_ENV}.json` as contracts repository
func (c *solarCLI) ContractsRepository() *contract.ContractsRepository {
	c.repoOnce.Do(func() {
		var repoFilePath string
		if *solarRepo != "" {
			repoFilePath = *solarRepo
		} else {
			repoFilePath = fmt.Sprintf("solar.%s.json", *solarEnv)
		}

		repo, err := contract.OpenContractsRepository(repoFilePath)
		if err != nil {
			fmt.Printf("error opening contracts repo file %s: %s\n", repoFilePath, err)
			os.Exit(1)
		}

		c.repo = repo
	})

	return c.repo
}

func (c *solarCLI) SbitRPC() *sbit.RPC {
	rpc, err := sbit.NewRPC(*sbitRPC)
	if err != nil {
		fmt.Println("Invalid SBIT RPC URL:", *sbitRPC)
		os.Exit(1)
	}

	return rpc
}

// ExpandJSONParams uses variable substitution syntax (e.g. $Foo, ${Foo}) to as placeholder for contract addresses
func (c *solarCLI) ExpandJSONParams(jsonParams string) string {
	repo := c.ContractsRepository()

	return varstr.Expand(jsonParams, func(key string) string {
		contract, found := repo.Get(key)
		if !found {
			panic(errors.Errorf("Invalid address expansion: %s", key))
		}

		return contract.Address.String()
	})
}

func (c *solarCLI) ConfigureBytesOutputFormat() {
	if *ethRPC != "" {
		contract.SetFormatBytesWithPrefix(true)
	}
}

func (c *solarCLI) Deployer() (deployer deployer.Deployer) {
	log := log.New(os.Stderr, "", log.Lshortfile)

	var err error
	var rpcURL *url.URL

	if rawurl := *sbitRPC; rawurl != "" {

		rpcURL, err = url.ParseRequestURI(rawurl)
		if err != nil {
			log.Fatalf("Invalid RPC url: %#v", rawurl)
		}
		deployer, err = sbit.NewDeployer(rpcURL, c.ContractsRepository(), *sbitSenderAddress)
	}

	if rawurl := *ethRPC; rawurl != "" {
		rpcURL, err = url.ParseRequestURI(rawurl)
		if err != nil {
			log.Fatalf("Invalid RPC url: %#v", rawurl)
		}

		deployer, err = eth.NewDeployer(rpcURL, c.ContractsRepository())
	}

	if deployer == nil {
		log.Fatalln(errorUnspecifiedRPC)
	}

	if err != nil {
		log.Fatalf("NewDeployer error %v", err)
	}

	return deployer
}

func Main() {
	cmdName, err := app.Parse(os.Args[1:])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	solar.ConfigureBytesOutputFormat()

	task := appTasks[cmdName]
	err = task()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

package setup

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
	"github.com/kwilteam/kwil-db/app/setup"
	"github.com/kwilteam/kwil-db/config"
	"github.com/kwilteam/kwil-db/core/crypto"
	"github.com/kwilteam/kwil-db/core/types"
	"github.com/kwilteam/kwil-db/node"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestConfig is the configuration for the test
type TestConfig struct {
	// REQUIRED: ClientDriver is the driver to use for the client
	ClientDriver ClientDriver
	// REQUIRED: Network is the network configuration
	Network *NetworkConfig
	// OPTIONAL: ContainerStartTimeout is the timeout for starting a container.
	// If not set, it will default to 30 seconds.
	ContainerStartTimeout time.Duration
}

func (c *TestConfig) ensureDefaults(t *testing.T) {
	if c.ContainerStartTimeout == 0 {
		c.ContainerStartTimeout = 30 * time.Second
	}

	if c.Network == nil {
		t.Fatal("Network is required")
	}

	if c.ClientDriver == "" {
		t.Fatal("ClientDriver is required")
	}

	c.Network.ensureDefaults(t)
}

// NetworkConfig is the configuration for a test network
type NetworkConfig struct {
	// REQUIRED: Nodes is the list of nodes in the network
	Nodes []*NodeConfig

	// REQUIRED: DBOwner is the initial wallet address that owns the database.
	DBOwner string

	// OPTIONAL: ConfigureGenesis is a function that alters the genesis configuration
	ConfigureGenesis func(*config.GenesisConfig)

	// OPTIONAL: ExtraServices are services that should be run with the test. The test
	// Automatically runs kwild and Postgres, but this allows for geth, kgw,
	// etc. to run as well.
	ExtraServices []*CustomService // TODO: we need more in this service definition struct. Will come back when I am farther along
	// TODO: we will probably need to add StateHash and MigrationParams when we add ZDT migration tests
}

func (n *NetworkConfig) ensureDefaults(t *testing.T) {
	if n.ConfigureGenesis == nil {
		n.ConfigureGenesis = func(*config.GenesisConfig) {}
	}

	if n.DBOwner == "" {
		t.Fatal("DBOwner is required")
	}

	if n.Nodes == nil {
		t.Fatal("Nodes is required")
	}
}

// NodeConfig is a configuration that allows external users to specify properties of the node
type NodeConfig struct {
	// OPTIONAL: DockerImage is the docker image to use
	// By default, it is "kwild:latest"
	DockerImage string
	// OPTIONAL: Validator is true if the node is a validator
	// By default, it is true.
	Validator bool

	// OPTIONAL: PrivateKey is the private key to use for the node.
	// If not set, a random key will be generated.
	PrivateKey *crypto.Secp256k1PrivateKey
	// OPTIONAL: Configure is a function that alter's the node's configuration
	Configure func(*config.Config)
}

// DefaultNodeConfig returns a default node configuration
func DefaultNodeConfig() *NodeConfig {
	pk, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		panic(err)
	}
	return &NodeConfig{
		DockerImage: "kwild:latest",
		Validator:   true,
		PrivateKey:  pk.(*crypto.Secp256k1PrivateKey),
		Configure:   func(*config.Config) {},
	}
}

// CustomNodeConfig provides a default node configuration that can be customized
func CustomNodeConfig(f func(*NodeConfig)) *NodeConfig {
	cfg := DefaultNodeConfig()
	f(cfg)
	return cfg
}

type Testnet struct {
	Nodes   []KwilNode
	testCtx *testingContext
}

// ExtraServiceEndpoint gets the endpoint for an extra service that was configured in the testnet
func (t *Testnet) ExtraServiceEndpoint(ctx context.Context, serviceName string, protocol string, port string) (string, error) {
	ct, ok := t.testCtx.containers[serviceName]
	if !ok {
		return "", fmt.Errorf("container not found")
	}

	exposed, _, err := getEndpoints(ct, ctx, nat.Port(port), protocol)
	return exposed, err
}

func SetupTests(t *testing.T, testConfig *TestConfig) *Testnet {
	testConfig.ensureDefaults(t)

	// we create a temporary directory to store the testnet configs
	tmpDir, err := os.MkdirTemp("", "TestKwilInt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Retaining data for failed test at path %v", tmpDir)
			return
		}
		os.RemoveAll(tmpDir)
	})
	ctx := context.Background()

	dockerNetwork, err := ensureNetworkExist(ctx, t.Name())
	require.NoError(t, err)

	t.Cleanup(func() {
		if !t.Failed() {
			t.Logf("teardown docker network %s from %s", dockerNetwork.Name, t.Name())
			err := dockerNetwork.Remove(ctx)
			require.NoErrorf(t, err, "failed to teardown network %s", dockerNetwork.Name)
		}
	})

	composePath, nodeInfo, err := generateCompose(dockerNetwork.Name, tmpDir, testConfig.Network.Nodes, testConfig.Network.ExtraServices, nil) //TODO: need user id and groups
	require.NoError(t, err)

	require.Equal(t, len(testConfig.Network.Nodes), len(nodeInfo)) // ensure that the number of nodes is the same as the number of node info
	if len(nodeInfo) == 0 {
		t.Fatal("at least one node is required")
	}

	genesisConfig := config.DefaultGenesisConfig()
	testConfig.Network.ConfigureGenesis(genesisConfig)

	generatedNodes := make([]*kwilNode, len(testConfig.Network.Nodes))
	testnetNodeConfigs := make([]*setup.TestnetNodeConfig, len(testConfig.Network.Nodes))
	serviceSet := map[string]struct{}{}
	servicesToRun := []*serviceDefinition{}
	for i, nodeCfg := range testConfig.Network.Nodes {
		var firstNode *kwilNode
		if i == 0 {
			firstNode = nil
		} else {
			firstNode = generatedNodes[0]
		}

		generatedNodes[i], err = nodeCfg.makeNode(nodeInfo[i], i == 0, firstNode)
		require.NoError(t, err)

		// ensure unique service names for kwild and Postgres
		_, ok := serviceSet[nodeInfo[i].KwilNodeServiceName]
		require.Falsef(t, ok, "duplicate service name %s", nodeInfo[i].KwilNodeServiceName)
		serviceSet[nodeInfo[i].KwilNodeServiceName] = struct{}{}

		_, ok = serviceSet[nodeInfo[i].PostgresServiceName]
		require.Falsef(t, ok, "duplicate service name %s", nodeInfo[i].PostgresServiceName)
		serviceSet[nodeInfo[i].PostgresServiceName] = struct{}{}

		// we append two services for each node: kwild and Postgres
		// kwild:
		servicesToRun = append(servicesToRun, &serviceDefinition{
			Name:    nodeInfo[i].KwilNodeServiceName,
			WaitMsg: &kwildWaitMsg,
		})
		// Postgres:
		servicesToRun = append(servicesToRun, &serviceDefinition{
			Name:    nodeInfo[i].PostgresServiceName,
			WaitMsg: &postgresWaitMsg,
		})

		// if i == 0, then it is the first node and will be the leader.
		// All nodes that are validators, including the leader, will be added to the Validator list
		if i == 0 {
			if !nodeCfg.Validator {
				t.Fatal("first node must be a validator")
			}

			genesisConfig.Leader = nodeCfg.PrivateKey.Public().Bytes()
		}
		if nodeCfg.Validator {
			genesisConfig.Validators = append(genesisConfig.Validators, &types.Validator{
				PubKey: nodeCfg.PrivateKey.Public().Bytes(),
				Power:  1,
			})
		}

		testnetNodeConfigs[i] = &setup.TestnetNodeConfig{
			PrivateKey: nodeCfg.PrivateKey,
			DirName:    generatedNodes[i].generatedInfo.KwilNodeServiceName,
			Config:     generatedNodes[i].config,
		}
	}

	require.NoError(t, genesisConfig.SanityChecks())

	// validate the user-provided services
	for _, svc := range testConfig.Network.ExtraServices {
		_, ok := serviceSet[svc.ServiceName]
		require.Falsef(t, ok, "duplicate service name %s", svc.ServiceName)
		serviceSet[svc.ServiceName] = struct{}{}

		var waitMsg *string
		if svc.WaitMsg != "" {
			waitMsg = &svc.WaitMsg
		}

		servicesToRun = append(servicesToRun, &serviceDefinition{
			Name:    svc.ServiceName,
			WaitMsg: waitMsg,
		})
	}

	err = setup.GenerateTestnetDir(tmpDir, genesisConfig, testnetNodeConfigs)
	require.NoError(t, err)

	testCtx := &testingContext{
		config:     testConfig,
		containers: make(map[string]*testcontainers.DockerContainer),
	}

	runDockerCompose(ctx, t, testCtx, composePath, servicesToRun)

	tp := &Testnet{
		testCtx: testCtx,
	}
	for _, node := range generatedNodes {
		node.testCtx = testCtx
		tp.Nodes = append(tp.Nodes, node)
	}

	return tp
}

var (
	kwildWaitMsg    string = "Committed Block"
	postgresWaitMsg string = `listening on IPv4 address "0.0.0.0", port 5432`
)

// serviceDefinition is a definition of a service in a docker-compose file
type serviceDefinition struct {
	Name    string
	WaitMsg *string // if nil, no wait
}

// runDockerCompose runs docker-compose with the given compose file
func runDockerCompose(ctx context.Context, t *testing.T, testCtx *testingContext, composePath string, services []*serviceDefinition) {
	var dc compose.ComposeStack
	var err error
	dc, err = compose.NewDockerCompose(composePath)
	require.NoError(t, err)

	ctxUp, cancel := context.WithCancel(ctx)

	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("Stopping but keeping containers for inspection after failed test: %v", dc.Services())
			cancel() // Stop, not Down, which would remove the containers too --- this doesn't work, dang
			time.Sleep(5 * time.Second)

			// There is no dc.Stop, but there should be! Do this instead:
			svcs := dc.Services()
			slices.Sort(svcs)
			for _, svc := range svcs {
				ct, err := dc.ServiceContainer(ctx, svc)
				if err != nil {
					t.Logf("could not get container %v: %v", svc, err)
					continue
				}
				err = ct.Stop(ctx, nil)
				if err != nil {
					t.Logf("could not stop container %v: %v", svc, err)
				}
			}
			return
		}
		t.Logf("teardown %s", dc.Services())
		err := dc.Down(ctx, compose.RemoveVolumes(true))
		require.NoErrorf(t, err, "failed to teardown %s", dc.Services())
		cancel() // no context leak
	})

	serviceNames := make([]string, len(services))
	for i, svc := range services {
		if svc.WaitMsg != nil {
			// wait for the service to be ready
			dc = dc.WaitForService(svc.Name, wait.NewLogStrategy(*svc.WaitMsg).WithStartupTimeout(testCtx.config.ContainerStartTimeout))
		}
		serviceNames[i] = svc.Name
	}

	err = dc.Up(ctxUp, compose.Wait(true), compose.RunServices(serviceNames...))
	t.Log("docker-compose up done")
	// wait as some protection against RPC errors with chain_info.
	// This was in the old tests, so I retain it here.
	time.Sleep(3 * time.Second)
	require.NoError(t, err)

	for _, svc := range services {
		ct, err := dc.ServiceContainer(ctx, svc.Name)
		require.NoError(t, err)
		require.NotNil(t, ct)
		testCtx.containers[svc.Name] = ct
	}
}

// makeNode prepares a node for the test network.
// It takes the node's config specified by the user, the node's info generated as part
// of the network setup, and the first node's info (used for bootstrapping).
func (c *NodeConfig) makeNode(generated *generatedNodeInfo, isFirstNode bool, firstNode *kwilNode) (*kwilNode, error) {
	defaultConf := config.DefaultConfig()
	conf := config.DefaultConfig()
	c.Configure(conf)

	// there are some configurations that the user cannot set, as they will screw up the test.
	// These are:
	// --admin.listen
	// --rpc.listen
	// --p2p.ip
	// --p2p.port
	// --db.host
	// --db.port
	// --db.user
	// --db.password
	// --db.name
	// --p2p.bootnodes
	ensureEq := func(name string, a, b interface{}) error {
		if a != b {
			return fmt.Errorf("configuration %s cannot be custom configured in tests", name)
		}
		return nil
	}
	err := errors.Join(
		ensureEq("admin.listen", conf.Admin.ListenAddress, defaultConf.Admin.ListenAddress),
		ensureEq("rpc.listen", conf.RPC.ListenAddress, defaultConf.RPC.ListenAddress),
		ensureEq("p2p.ip", conf.P2P.IP, defaultConf.P2P.IP),
		ensureEq("p2p.port", conf.P2P.Port, defaultConf.P2P.Port),
		ensureEq("db.host", conf.DB.Host, defaultConf.DB.Host),
		ensureEq("db.port", conf.DB.Port, defaultConf.DB.Port),
		ensureEq("db.user", conf.DB.User, defaultConf.DB.User),
		ensureEq("db.password", conf.DB.Pass, defaultConf.DB.Pass),
		ensureEq("db.name", conf.DB.DBName, defaultConf.DB.DBName),
		ensureEq("p2p.bootnodes", conf.P2P.BootNodes, defaultConf.P2P.BootNodes),
	)
	if err != nil {
		return nil, err
	}

	// these configurations set here will be combined with the configs hard-coded
	// in node-compose.yml.template. There, we hardcore things like Postgres connection
	// info, rpc endpoints (which don't concern us since the container maps ports to the host),
	// and other things that are not relevant to the test.

	// setting p2p configs
	if !isFirstNode {
		// if this is not the first node, we should set the first node as the bootnode
		conf.P2P.BootNodes = []string{node.FormatPeerString(firstNode.nodeTestConfig.PrivateKey.Public().Bytes(), firstNode.nodeTestConfig.PrivateKey.Public().Type(), firstNode.generatedInfo.KwilNodeServiceName, p2pPort)}
	}

	return &kwilNode{
		config:         conf,
		nodeTestConfig: c,
		generatedInfo:  generated,
	}, nil
}

type kwilNode struct {
	config         *config.Config
	nodeTestConfig *NodeConfig
	testCtx        *testingContext
	generatedInfo  *generatedNodeInfo
	client         JSONRPCClient
}

type testingContext struct {
	config     *TestConfig
	containers map[string]*testcontainers.DockerContainer
}

func (k *kwilNode) PrivateKey() *crypto.Secp256k1PrivateKey {
	return k.nodeTestConfig.PrivateKey
}

func (k *kwilNode) PublicKey() *crypto.Secp256k1PublicKey {
	return k.nodeTestConfig.PrivateKey.Public().(*crypto.Secp256k1PublicKey)
}

func (k *kwilNode) IsValidator() bool {
	return k.nodeTestConfig.Validator
}

func (k *kwilNode) Config() *config.Config {
	return k.config
}

func (k *kwilNode) JSONRPCClient(t *testing.T, ctx context.Context, usingGateway bool) JSONRPCClient {
	if k.client != nil {
		return k.client
	}

	container, ok := k.testCtx.containers[k.generatedInfo.KwilNodeServiceName]
	if !ok {
		t.Fatalf("container %s not found", k.generatedInfo.KwilNodeServiceName)
	}

	endpoint, err := kwildJSONRPCEndpoints(container, ctx)
	require.NoError(t, err)

	client, err := getNewClientFn(k.testCtx.config.ClientDriver)(ctx, endpoint, usingGateway, t.Logf)
	require.NoError(t, err)

	k.client = client
	return client
}

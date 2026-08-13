package gateway

import (
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
)

func TestDomainGatewaySSHForwardIntegration(t *testing.T) {
	host := os.Getenv("LION_SSH_TEST_HOST")
	keyFile := os.Getenv("LION_SSH_TEST_KEY_FILE")
	destination := os.Getenv("LION_SSH_TEST_DESTINATION")
	if host == "" || keyFile == "" || destination == "" {
		t.Skip("set LION_SSH_TEST_HOST, LION_SSH_TEST_KEY_FILE and LION_SSH_TEST_DESTINATION")
	}
	port, err := strconv.Atoi(os.Getenv("LION_SSH_TEST_PORT"))
	if err != nil || port == 0 {
		port = 22
	}
	secret, err := os.ReadFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	sshTarget := func(name, address string, sshPort int) *model.Gateway {
		return &model.Gateway{
			Name:      name,
			Address:   address,
			Protocols: model.Protocols{{Name: "ssh", Port: sshPort}},
			Account: model.Account{BaseAccount: model.BaseAccount{
				Username:   "root",
				Secret:     string(secret),
				SecretType: model.LabelValue{Value: "ssh_key"},
			}},
		}
	}
	forwarder := DomainGateway{
		DstAddr:     destination,
		Destination: sshTarget("integration-provider", host, port),
	}
	if jumpHost := os.Getenv("LION_SSH_TEST_JUMP_HOST"); jumpHost != "" {
		jumpPort, err := strconv.Atoi(os.Getenv("LION_SSH_TEST_JUMP_PORT"))
		if err != nil || jumpPort == 0 {
			jumpPort = 22
		}
		forwarder.SelectedGateway = sshTarget("integration-gateway", jumpHost, jumpPort)
	}
	if err := forwarder.Start(); err != nil {
		t.Fatal(err)
	}
	defer forwarder.Stop()

	conn, err := net.DialTimeout("tcp", forwarder.GetListenAddr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded destination: %v", err)
	}
	_ = conn.Close()
}

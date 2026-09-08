package session

import (
	"fmt"
	"time"

	"github.com/jumpserver-dev/sdk-go/httplib"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service/panda"
)

// PandaClient checks application errors in addition to HTTP errors.
type PandaClient struct {
	client *httplib.Client
}

func NewPandaClient(baseURL string, key model.AccessKey, insecure bool) *PandaClient {
	var options []httplib.Opt
	if insecure {
		options = append(options, httplib.WithInsecure())
	}
	client, err := httplib.NewClient(baseURL, 30*time.Second, options...)
	if err != nil {
		return nil
	}
	client.SetAuthSign(&panda.ProfileAuth{KeyID: key.ID, SecretID: key.Secret})
	client.SetHeader("X-JMS-ORG", "ROOT")
	return &PandaClient{client: client}
}

func (c *PandaClient) CreateContainer(token string, option model.VirtualAppOption) (model.VirtualAppContainer, error) {
	var response panda.Response
	_, err := c.client.Post(panda.ContainerCreateURL, map[string]interface{}{
		"token": token, "virtual_app": option,
	}, &response)
	if err != nil {
		return model.VirtualAppContainer{}, err
	}
	if !response.Success {
		return model.VirtualAppContainer{}, fmt.Errorf("Panda container creation failed: %s", response.Msg)
	}
	return response.Data, nil
}

func (c *PandaClient) ReleaseContainer(id string) error {
	_, err := c.client.Post(panda.ContainerReleaseURL, map[string]string{"container_id": id}, nil)
	return err
}

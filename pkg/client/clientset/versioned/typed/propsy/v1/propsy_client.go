/*
This file has been generated.
*/
// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/seznam/ProPsy/pkg/apis/propsy/v1"
	"github.com/seznam/ProPsy/pkg/client/clientset/versioned/scheme"
	serializer "k8s.io/apimachinery/pkg/runtime/serializer"
	rest "k8s.io/client-go/rest"
)

type PropsyV1Interface interface {
	RESTClient() rest.Interface
	ProPsyServicesGetter
}

// PropsyV1Client is used to interact with features provided by the propsy.seznam.cz group.
type PropsyV1Client struct {
	restClient rest.Interface
}

func (c *PropsyV1Client) ProPsyServices(namespace string) ProPsyServiceInterface {
	return newProPsyServices(c, namespace)
}

// NewForConfig creates a new PropsyV1Client for the given config.
func NewForConfig(c *rest.Config) (*PropsyV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &PropsyV1Client{client}, nil
}

// NewForConfigOrDie creates a new PropsyV1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *PropsyV1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new PropsyV1Client for the given RESTClient.
func New(c rest.Interface) *PropsyV1Client {
	return &PropsyV1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *PropsyV1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}

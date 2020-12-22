package authz

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"text/template"

	"github.com/ory/x/httpx"
	"github.com/pkg/errors"

	"github.com/ory/oathkeeper/driver/configuration"
	"github.com/ory/oathkeeper/helper"
	"github.com/ory/oathkeeper/pipeline"
	"github.com/ory/oathkeeper/pipeline/authn"
	"github.com/ory/oathkeeper/x"
)

// AuthorizerOPAConfiguration represents a configuration for the remote_json authorizer.
type AuthorizerOPAConfiguration struct {
	Remote string `json:"remote"`
}

// PayloadTemplateID returns a string with which to associate the payload template.
func (c *AuthorizerOPAConfiguration) PayloadTemplateID() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(c.Remote)))
}

// AuthorizerOPA implements the Authorizer interface.
type AuthorizerOPA struct {
	c configuration.Provider

	client *http.Client
	t      *template.Template
}

// NewAuthorizerOPA creates a new AuthorizerOPA.
func NewAuthorizerOPA(c configuration.Provider) *AuthorizerOPA {
	return &AuthorizerOPA{
		c:      c,
		client: httpx.NewResilientClientLatencyToleranceHigh(nil),
		t:      x.NewTemplate("opa"),
	}
}

// GetID implements the Authorizer interface.
func (a *AuthorizerOPA) GetID() string {
	return "opa"
}

// Authorize implements the Authorizer interface.
func (a *AuthorizerOPA) Authorize(r *http.Request, session *authn.AuthenticationSession, config json.RawMessage, _ pipeline.Rule) error {
	c, err := a.Config(config)
	if err != nil {
		return err
	}

	// Parse input fields for open policy agent
	bodyJson := map[string]interface{}{
		"input": map[string]interface{}{
			"path":   r.URL.Path,
			"method": r.Method,
			"token":  strings.ReplaceAll(r.Header.Get("authorization"), "Bearer ", ""),
		},
	}

	body, err := json.Marshal(bodyJson)
	if err != nil {
		return errors.WithStack(err)
	}

	req, err := http.NewRequest("POST", c.Remote, bytes.NewReader(body))
	if err != nil {
		return errors.WithStack(err)
	}
	req.Header.Add("Content-Type", "application/json")

	res, err := a.client.Do(req)
	if err != nil {
		return errors.WithStack(err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusForbidden {
		return errors.WithStack(helper.ErrForbidden)
	} else if res.StatusCode != http.StatusOK {
		return errors.Errorf("expected status code %d but got %d", http.StatusOK, res.StatusCode)
	}

	return nil
}

// Validate implements the Authorizer interface.
func (a *AuthorizerOPA) Validate(config json.RawMessage) error {
	if !a.c.AuthorizerIsEnabled(a.GetID()) {
		return NewErrAuthorizerNotEnabled(a)
	}

	_, err := a.Config(config)
	return err
}

// Config merges config and the authorizer's configuration and validates the
// resulting configuration. It reports an error if the configuration is invalid.
func (a *AuthorizerOPA) Config(config json.RawMessage) (*AuthorizerOPAConfiguration, error) {
	var c AuthorizerOPAConfiguration
	if err := a.c.AuthorizerConfig(a.GetID(), config, &c); err != nil {
		return nil, NewErrAuthorizerMisconfigured(a, err)
	}

	return &c, nil
}

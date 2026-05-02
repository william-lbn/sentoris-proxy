package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type MonitorResponse struct {
	Status       string `json:"status"`
	RequestCount int    `json:"request_count"`
	Uptime       string `json:"uptime"`
}

type MetricsResponse struct {
	RequestCount int     `json:"request_count"`
	Uptime       string  `json:"uptime"`
	TotalCost    float64 `json:"total_cost"`
	SuccessRate  float64 `json:"success_rate"`
	Router       struct {
		ModelsCount   int `json:"models_count"`
		ProvidersCount int `json:"providers_count"`
		Status        string `json:"status"`
	} `json:"router"`
	Trace struct {
		Count int `json:"count"`
		Stats struct {
			ByModel map[string]int `json:"by_model"`
			ByState map[string]int `json:"by_state"`
			Total   int            `json:"total"`
			TotalCost float64      `json:"total_cost"`
		} `json:"stats"`
		Status string `json:"status"`
	} `json:"trace"`
	Budget struct {
		RemainingUSD float64 `json:"remaining_usd"`
		Status       string  `json:"status"`
	} `json:"budget"`
}

type ModelsResponse struct {
	Models []struct {
		Provider struct {
			BaseURL    string   `json:"base_url"`
			IsDefault  bool     `json:"is_default"`
			Models     []string `json:"models"`
			Name       string   `json:"name"`
			Pricing    struct {
				InputPer1K  float64 `json:"input_per_1k"`
				OutputPer1K float64 `json:"output_per_1k"`
			} `json:"pricing"`
			Status string `json:"status"`
		} `json:"provider"`
	} `json:"models"`
	TotalModels   []string `json:"total_models"`
	TotalProviders int     `json:"total_providers"`
}

type ExtensionsResponse struct {
	Extensions []struct {
		Namespace          string `json:"namespace"`
		Version            string `json:"version"`
		Title              string `json:"title"`
		Status             string `json:"status"`
		Maintainer         string `json:"maintainer"`
		SpecificationURI   string `json:"specification_uri"`
		MinProtocolVersion string `json:"min_protocol_version"`
		Tags               []string `json:"tags"`
		HandlerClass       string `json:"handler_class"`
		Handler            string `json:"handler"`
	} `json:"extensions"`
}

type TracesResponse struct {
	Traces       []TraceItem `json:"traces"`
	DisplayCount int         `json:"display_count"`
	TotalCount   int         `json:"total_count"`
}

type TraceItem struct {
	TraceID     string  `json:"trace_id"`
	Model       string  `json:"model"`
	Status      string  `json:"status"`
	CostUSD     float64 `json:"cost_usd"`
	TokensCount int     `json:"tokens_count"`
	CreatedAt   string  `json:"created_at"`
}

type BudgetResponse struct {
	ConsumedUSD  float64       `json:"consumed_usd"`
	RemainingUSD float64       `json:"remaining_usd"`
	TotalLimitUSD float64      `json:"total_limit_usd"`
	Status       string        `json:"status"`
	Transactions []Transaction `json:"transactions"`
}

type Transaction struct {
	ID        string  `json:"id"`
	Model     string  `json:"model"`
	CostUSD   float64 `json:"cost_usd"`
	Status    string  `json:"status"`
	Timestamp string  `json:"timestamp"`
}

type ProviderConfig struct {
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	APIKey         string `json:"api_key"`
	DefaultModel   string `json:"default_model"`
	Models         []string `json:"models"`
	IsDefault      bool   `json:"is_default"`
}

func (c *Client) GetMonitor() (*MonitorResponse, error) {
	result := &MonitorResponse{}
	_, err := c.getJSON("/v1/monitor", result)
	return result, err
}

func (c *Client) GetMetrics() (*MetricsResponse, error) {
	result := &MetricsResponse{}
	_, err := c.getJSON("/v1/monitor/metrics", result)
	return result, err
}

func (c *Client) GetModels() (*ModelsResponse, error) {
	result := &ModelsResponse{}
	_, err := c.getJSON("/v1/monitor/models", result)
	return result, err
}

func (c *Client) GetExtensions() (*ExtensionsResponse, error) {
	result := &ExtensionsResponse{}
	_, err := c.getJSON("/v1/monitor/extensions", result)
	return result, err
}

func (c *Client) GetTraces() (*TracesResponse, error) {
	result := &TracesResponse{}
	_, err := c.getJSON("/v1/monitor/traces", result)
	return result, err
}

func (c *Client) GetBudget() (*BudgetResponse, error) {
	result := &BudgetResponse{}
	_, err := c.getJSON("/v1/monitor/budget", result)
	return result, err
}

func (c *Client) GetProviders() (*ModelsResponse, error) {
	result := &ModelsResponse{}
	_, err := c.getJSON("/v1/monitor/models", result)
	return result, err
}

func (c *Client) AddProvider(config *ProviderConfig) error {
	return c.postJSON("/v1/admin/providers", config, nil)
}

func (c *Client) UpdateProvider(name string, config *ProviderConfig) error {
	return c.putJSON(fmt.Sprintf("/v1/admin/providers/%s", name), config, nil)
}

func (c *Client) DeleteProvider(name string) error {
	return c.delete(fmt.Sprintf("/v1/admin/providers/%s", name))
}

func (c *Client) getJSON(path string, result interface{}) (interface{}, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API request failed: %s - %s", resp.Status, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) postJSON(path string, body interface{}, result interface{}) error {
	return c.doJSON("POST", path, body, result)
}

func (c *Client) putJSON(path string, body interface{}, result interface{}) error {
	return c.doJSON("PUT", path, body, result)
}

func (c *Client) doJSON(method, path string, body, result interface{}) error {
	req, err := http.NewRequest(method, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API request failed: %s - %s", resp.Status, string(bodyBytes))
	}

	if result != nil {
		return json.NewDecoder(resp.Body).Decode(result)
	}
	return nil
}

func (c *Client) delete(path string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API request failed: %s - %s", resp.Status, string(body))
	}

	return nil
}

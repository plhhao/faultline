package control

type ListenerStatus struct {
	ProxyID string `json:"proxy_id"`
	Address string `json:"address"`
	Ready   bool   `json:"ready"`
}

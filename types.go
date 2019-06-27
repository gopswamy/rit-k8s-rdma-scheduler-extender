package main

type PF struct {
	UsedTxRate     int   `json:"used_tx_rate"`
	CapacityTxRate int   `json:"capacity_tx_rate"`
	VFs            []*VF `json:"vfs"`
}

type VF struct {
	MaxTxRate int `json:"max_tx_rate"`
	MinTxRate int `json:"min_tx_rate"`
}

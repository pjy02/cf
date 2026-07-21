package model

import "time"

const (
	CarrierCT = "ct"
	CarrierCM = "cm"
	CarrierCU = "cu"
)

var CarrierOrder = []string{CarrierCM, CarrierCU, CarrierCT}

var CarrierNames = map[string]string{
	CarrierCM: "中国移动",
	CarrierCU: "中国联通",
	CarrierCT: "中国电信",
}

type Node struct {
	IP         string  `json:"ip"`
	Speed      float64 `json:"speed"`
	Latency    float64 `json:"latency"`
	Region     string  `json:"region,omitempty"`
	Carrier    string  `json:"carrier"`
	SourceTime string  `json:"source_time,omitempty"`
}

type Snapshot struct {
	Carrier    string `json:"carrier"`
	SourceTime string `json:"source_time,omitempty"`
	Found      bool   `json:"found"`
	Nodes      []Node `json:"nodes"`
}

type SourceData struct {
	FetchedAt time.Time           `json:"fetched_at"`
	Carriers  map[string]Snapshot `json:"carriers"`
	Warnings  []string            `json:"warnings,omitempty"`
}

// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rpc

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgentID_UnmarshalJSON(t *testing.T) {
	var id AgentID
	err := json.Unmarshal([]byte(`"alice"`), &id)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if id != "alice" {
		t.Errorf("AgentID = %q, want %q", id, "alice")
	}
}

func TestAgentID_invalid(t *testing.T) {
	var id AgentID
	err := json.Unmarshal([]byte("not-a-string"), &id)
	if err == nil {
		t.Error("expected error for non-string JSON")
	}
}

func TestAgentInfo_UnmarshalJSON(t *testing.T) {
	data := `{"auth_status":"ok","available":true,"id":"alice","model":"gpt-4"}`
	var info AgentInfo
	err := json.Unmarshal([]byte(data), &info)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if info.ID != "alice" {
		t.Errorf("ID = %q, want %q", info.ID, "alice")
	}
	if info.AuthStatus != AgentInfoAuthStatusOK {
		t.Errorf("AuthStatus = %q, want %q", info.AuthStatus, AgentInfoAuthStatusOK)
	}
	if info.Available != true {
		t.Errorf("Available = %v, want true", info.Available)
	}
	if info.Model == nil || *info.Model != "gpt-4" {
		t.Errorf("Model = %v, want %q", info.Model, "gpt-4")
	}
}

func TestAgentInfo_UnmarshalJSON_missingRequired(t *testing.T) {
	for _, field := range []string{"auth_status", "available", "id"} {
		data := `{"auth_status":"ok","available":true,"id":"alice"}`
		// Create a map and delete the field to be sure.
		var m map[string]interface{}
		json.Unmarshal([]byte(data), &m)
		delete(m, field)
		bytes, _ := json.Marshal(m)

		var info AgentInfo
		err := json.Unmarshal(bytes, &info)
		if err == nil {
			t.Errorf("expected error when field %q is missing", field)
		}
	}
}

func TestAgentInfoAuthStatus_invalid(t *testing.T) {
	var status AgentInfoAuthStatus
	err := json.Unmarshal([]byte(`"bogus"`), &status)
	if err == nil {
		t.Error("expected error for invalid auth status")
	}
}

func TestAggregateContext_UnmarshalJSON(t *testing.T) {
	data := `{
		"agents": [
			{"agent_id":"a1","files_in_context":[{"bytes":100,"path":"/a.go"}]}
		],
		"totals": {
			"active_agents":1,"cached":0,"cost_usd":0.01,"errors_5m":0,"tokens_in":100,"tokens_out":50
		}
	}`
	var ctx AggregateContext
	err := json.Unmarshal([]byte(data), &ctx)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(ctx.Agents) != 1 {
		t.Errorf("len(Agents) = %d, want 1", len(ctx.Agents))
	}
	if ctx.Totals.CostUsd != 0.01 {
		t.Errorf("CostUsd = %f, want 0.01", ctx.Totals.CostUsd)
	}
}

func TestAggregateContext_totalsValidation(t *testing.T) {
	fields := []string{"active_agents", "cached", "cost_usd", "errors_5m", "tokens_in", "tokens_out"}
	for _, field := range fields {
		// Start with valid data.
		m := map[string]interface{}{
			"active_agents": 1,
			"cached":        0,
			"cost_usd":      0.0,
			"errors_5m":     0,
			"tokens_in":     0,
			"tokens_out":    0,
		}
		m[field] = -1 // Make it invalid.
		data := map[string]interface{}{
			"agents": []interface{}{},
			"totals": m,
		}
		bytes, _ := json.Marshal(data)

		var ctx AggregateContext
		err := json.Unmarshal(bytes, &ctx)
		if err == nil {
			t.Errorf("expected error for negative %s", field)
		}
	}
}

func TestChartKind_valid(t *testing.T) {
	for _, kind := range []string{"donut", "sparkline", "bars", "line"} {
		var ck ChartKind
		err := json.Unmarshal([]byte(`"`+kind+`"`), &ck)
		if err != nil {
			t.Errorf("UnmarshalJSON(%q): %v", kind, err)
		}
	}
}

func TestChartKind_invalid(t *testing.T) {
	var ck ChartKind
	err := json.Unmarshal([]byte(`"pie"`), &ck)
	if err == nil {
		t.Error("expected error for invalid chart kind")
	}
}

func TestMetricKind_valid(t *testing.T) {
	for _, kind := range []string{"counter", "histogram", "gauge"} {
		var mk MetricKind
		err := json.Unmarshal([]byte(`"`+kind+`"`), &mk)
		if err != nil {
			t.Errorf("UnmarshalJSON(%q): %v", kind, err)
		}
	}
}

func TestMetricKind_invalid(t *testing.T) {
	var mk MetricKind
	err := json.Unmarshal([]byte(`"invalid"`), &mk)
	if err == nil {
		t.Error("expected error for invalid metric kind")
	}
}

func TestContextSnapshot_UnmarshalJSON(t *testing.T) {
	data := `{
		"agent_id":"a1",
		"turn":5,
		"tokens":{"cached":10,"input":100,"output":50},
		"tools":[{"name":"Read"}]
	}`
	var snap ContextSnapshot
	err := json.Unmarshal([]byte(data), &snap)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if snap.AgentID != "a1" {
		t.Errorf("AgentID = %q, want %q", snap.AgentID, "a1")
	}
	if snap.Turn == nil || *snap.Turn != 5 {
		t.Errorf("Turn = %v, want 5", snap.Turn)
	}
	if snap.Tokens == nil {
		t.Fatal("Tokens is nil")
	}
	if snap.Tokens.Input != 100 {
		t.Errorf("Tokens.Input = %d, want 100", snap.Tokens.Input)
	}
	if len(snap.Tools) != 1 || snap.Tools[0].Name != "Read" {
		t.Errorf("Tools = %v, want [Read]", snap.Tools)
	}
}

func TestContextSnapshot_negativeFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
	}{
		{"cost_usd", `{"agent_id":"a","cost_usd":-1}`},
		{"errors5m", `{"agent_id":"a","errors_5m":-1}`},
		{"turn", `{"agent_id":"a","turn":-1}`},
	} {
		var snap ContextSnapshot
		err := json.Unmarshal([]byte(tc.json), &snap)
		if err == nil {
			t.Errorf("expected error for %s", tc.name)
		}
	}
}

func TestBucket_UnmarshalJSON(t *testing.T) {
	ts := time.Now().UTC().Truncate(time.Second)
	tsStr := ts.Format(time.RFC3339)
	data := `{"ts":"` + tsStr + `","value":42.5}`
	var bucket Bucket
	err := json.Unmarshal([]byte(data), &bucket)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if bucket.Value == nil || *bucket.Value != 42.5 {
		t.Errorf("Value = %v, want 42.5", bucket.Value)
	}
}

func TestBucket_negativeCount(t *testing.T) {
	var bucket Bucket
	err := json.Unmarshal([]byte(`{"ts":"2024-01-01T00:00:00Z","count":-1}`), &bucket)
	if err == nil {
		t.Error("expected error for negative count")
	}
}

func TestDonutChart_UnmarshalJSON(t *testing.T) {
	data := `{
		"data_hash":"abc123",
		"kind":"donut",
		"segments":[{"label":"CPU","value":80}]
	}`
	var chart DonutChart
	err := json.Unmarshal([]byte(data), &chart)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if chart.Kind != "donut" {
		t.Errorf("Kind = %q, want %q", chart.Kind, "donut")
	}
	if len(chart.Segments) != 1 {
		t.Errorf("len(Segments) = %d, want 1", len(chart.Segments))
	}
}

func TestDonutChart_wrongKind(t *testing.T) {
	var chart DonutChart
	err := json.Unmarshal([]byte(`{"data_hash":"x","kind":"bars","segments":[]}`), &chart)
	if err == nil {
		t.Error("expected error for wrong kind")
	}
}

func TestBarsChart_UnmarshalJSON(t *testing.T) {
	data := `{
		"data_hash":"xyz",
		"kind":"bars",
		"series":[{"label":"Q1","values":[1,2,3]}],
		"x_labels":["Jan"]
	}`
	var chart BarsChart
	err := json.Unmarshal([]byte(data), &chart)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(chart.Series) != 1 {
		t.Errorf("len(Series) = %d, want 1", len(chart.Series))
	}
	if chart.Series[0].Label != "Q1" {
		t.Errorf("Series[0].Label = %q, want %q", chart.Series[0].Label, "Q1")
	}
}

func TestLineChart_UnmarshalJSON(t *testing.T) {
	data := `{
		"data_hash":"l123",
		"kind":"line",
		"series":[{"label":"Temp","points":[{"x":1,"y":20}]}]
	}`
	var chart LineChart
	err := json.Unmarshal([]byte(data), &chart)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if len(chart.Series) != 1 {
		t.Fatalf("len(Series) = %d, want 1", len(chart.Series))
	}
	if len(chart.Series[0].Points) != 1 {
		t.Fatalf("len(Points) = %d, want 1", len(chart.Series[0].Points))
	}
	if chart.Series[0].Points[0].Y != 20 {
		t.Errorf("Points[0].Y = %f, want 20", chart.Series[0].Points[0].Y)
	}
}

func TestPingResult_UnmarshalJSON(t *testing.T) {
	data := `{"pong":true,"proto":{"major":0,"minor":1},"uptime_s":100.5,"version":"1.0"}`
	var result PingResult
	err := json.Unmarshal([]byte(data), &result)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if !result.Pong {
		t.Error("Pong = false, want true")
	}
}

func TestHint_valid(t *testing.T) {
	for _, hint := range []string{"ok", "warn", "err", "accent", "dim"} {
		var h Hint
		err := json.Unmarshal([]byte(`"`+hint+`"`), &h)
		if err != nil {
			t.Errorf("UnmarshalJSON(%q): %v", hint, err)
		}
	}
}

func TestHint_invalid(t *testing.T) {
	var h Hint
	err := json.Unmarshal([]byte(`"bold"`), &h)
	if err == nil {
		t.Error("expected error for invalid hint")
	}
}

func TestRollupTier_valid(t *testing.T) {
	for _, tier := range []string{"raw", "hourly", "daily", "weekly", "monthly"} {
		var rt RollupTier
		err := json.Unmarshal([]byte(`"`+tier+`"`), &rt)
		if err != nil {
			t.Errorf("UnmarshalJSON(%q): %v", tier, err)
		}
	}
}

func TestStatus_UnmarshalJSON(t *testing.T) {
	data := `{
		"active_agent": "a1",
		"cost_usd": 0.05,
		"errors_5m": 0,
		"proto": {"major": 0, "minor": 1},
		"quota_pct": 50.0,
		"tokens_in": 1000,
		"tokens_out": 500,
		"turn": 10
	}`
	var st Status
	err := json.Unmarshal([]byte(data), &st)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if st.ActiveAgent != "a1" {
		t.Errorf("ActiveAgent = %v, want %q", st.ActiveAgent, "a1")
	}
}

func TestContextSnapshotFilesInContext_validation(t *testing.T) {
	var elem ContextSnapshotFilesInContextElem
	err := json.Unmarshal([]byte(`{"bytes":-1,"path":"/a.go"}`), &elem)
	if err == nil {
		t.Error("expected error for negative bytes")
	}
}

func TestContextSnapshotMCPServers_UnmarshalJSON(t *testing.T) {
	data := `{"connected":true,"name":"files","tool_count":3}`
	var elem ContextSnapshotMCPServersElem
	err := json.Unmarshal([]byte(data), &elem)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if elem.ToolCount != 3 {
		t.Errorf("ToolCount = %d, want 3", elem.ToolCount)
	}
}

func TestContextSnapshotMCPServers_negativeToolCount(t *testing.T) {
	var elem ContextSnapshotMCPServersElem
	err := json.Unmarshal([]byte(`{"connected":true,"name":"x","tool_count":-1}`), &elem)
	if err == nil {
		t.Error("expected error for negative tool_count")
	}
}

func TestContextSnapshotTokens_UnmarshalJSON(t *testing.T) {
	data := `{"cached":10,"input":100,"output":50}`
	var tok ContextSnapshotTokens
	err := json.Unmarshal([]byte(data), &tok)
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if tok.Input != 100 || tok.Output != 50 || tok.Cached != 10 {
		t.Errorf("Tokens = %+v, want {Input:100 Output:50 Cached:10}", tok)
	}
}

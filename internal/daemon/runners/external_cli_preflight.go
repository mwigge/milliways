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

package runners

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/clientprofiles"
)

const (
	externalCLIPreflightWarnCode  = -32030
	externalCLIPreflightBlockCode = -32031
)

type externalCLIPreflightResult struct {
	Mode     security.Mode
	Warnings []clientprofiles.ProfileWarning
	Err      error
}

var externalCLIPreflightCheck = defaultExternalCLIPreflightCheck

func runExternalCLIPreflight(ctx context.Context, agentID, cwd string, stream Pusher, metrics MetricsObserver) bool {
	result := externalCLIPreflightCheck(ctx, agentID, cwd)
	mode := security.NormalizeMode(result.Mode)
	if mode == security.ModeOff || mode == security.ModeObserve {
		return true
	}
	if result.Err != nil {
		stream.Push(map[string]any{
			"t":     "warn",
			"agent": agentID,
			"code":  externalCLIPreflightWarnCode,
			"msg":   agentID + ": security profile warning before handoff: " + scrubBearer(result.Err.Error()),
		})
		return true
	}
	if len(result.Warnings) == 0 {
		return true
	}

	blocking := externalCLIPreflightBlockingWarnings(result.Warnings)
	if (mode == security.ModeStrict || mode == security.ModeCI) && len(blocking) > 0 {
		observeError(metrics, agentID)
		stream.Push(map[string]any{
			"t":        "err",
			"agent":    agentID,
			"code":     externalCLIPreflightBlockCode,
			"category": string(security.FindingClient),
			"msg":      externalCLIPreflightMessage(agentID, "security profile blocked handoff", blocking),
			"findings": externalCLIPreflightFindings(blocking),
		})
		return false
	}

	stream.Push(map[string]any{
		"t":        "warn",
		"agent":    agentID,
		"code":     externalCLIPreflightWarnCode,
		"category": string(security.FindingClient),
		"msg":      externalCLIPreflightMessage(agentID, "security profile warning before handoff", result.Warnings),
		"findings": externalCLIPreflightFindings(result.Warnings),
	})
	return true
}

func defaultExternalCLIPreflightCheck(ctx context.Context, agentID, cwd string) externalCLIPreflightResult {
	mode := externalCLIPreflightMode(ctx, agentID, cwd)
	if mode == security.ModeOff || mode == security.ModeObserve {
		return externalCLIPreflightResult{Mode: mode}
	}

	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	result := clientprofiles.New(agentID, clientprofiles.DefaultOptions()).Check(checkCtx, cwd)
	out := externalCLIPreflightResult{
		Mode:     mode,
		Warnings: result.Warnings,
	}
	if result.Error != "" {
		out.Err = context.Cause(checkCtx)
		if out.Err == nil {
			out.Err = errString(result.Error)
		}
	}
	return out
}

func externalCLIPreflightMode(ctx context.Context, agentID, cwd string) security.Mode {
	mode := security.NormalizeMode(security.Mode(strings.TrimSpace(os.Getenv("MILLIWAYS_SECURITY_MODE"))))
	if fw := commandFirewallForAgentWorkspace(agentID, cwd); fw != nil {
		result, err := fw.EvaluateCommand(ctx, CommandFirewallRequest{
			Command:   "true",
			ToolName:  "external-cli-preflight",
			SessionID: agentID,
		})
		if err == nil {
			mode = security.NormalizeMode(result.Mode)
		}
	}
	return mode
}

func externalCLIPreflightBlockingWarnings(warnings []clientprofiles.ProfileWarning) []clientprofiles.ProfileWarning {
	var out []clientprofiles.ProfileWarning
	for _, warning := range warnings {
		if warning.Severity == clientprofiles.SeverityCritical {
			out = append(out, warning)
		}
	}
	return out
}

func externalCLIPreflightMessage(agentID, action string, warnings []clientprofiles.ProfileWarning) string {
	ids := make([]string, 0, min(len(warnings), 3))
	for i, warning := range warnings {
		if i >= 3 {
			break
		}
		if warning.ID != "" {
			ids = append(ids, warning.ID)
		}
	}
	if len(ids) == 0 {
		return agentID + ": " + action
	}
	msg := agentID + ": " + action + ": " + strings.Join(ids, ", ")
	if len(warnings) > len(ids) {
		msg += " +" + strconv.Itoa(len(warnings)-len(ids))
	}
	return msg
}

func externalCLIPreflightFindings(warnings []clientprofiles.ProfileWarning) []map[string]any {
	out := make([]map[string]any, 0, len(warnings))
	for _, warning := range warnings {
		item := map[string]any{
			"client":   warning.Client,
			"id":       warning.ID,
			"severity": string(warning.Severity),
			"summary":  warning.Summary,
		}
		if warning.Detail != "" {
			item["detail"] = warning.Detail
		}
		if warning.Path != "" {
			item["path"] = warning.Path
		}
		if warning.Key != "" {
			item["key"] = warning.Key
		}
		out = append(out, item)
	}
	return out
}

type errString string

func (e errString) Error() string { return string(e) }

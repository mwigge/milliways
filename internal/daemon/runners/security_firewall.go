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

import "sync"

// CommandFirewallProvider returns the current command firewall for a runner.
// It is configured by the daemon so macOS and Linux app launches share the same
// persisted workspace security policy.
type CommandFirewallProvider func(agentID string) CommandFirewall

var commandFirewallProvider struct {
	mu sync.RWMutex
	fn CommandFirewallProvider
}

// SetCommandFirewallProvider configures the runtime firewall provider. Passing
// nil disables runtime command firewall injection.
func SetCommandFirewallProvider(fn CommandFirewallProvider) {
	commandFirewallProvider.mu.Lock()
	defer commandFirewallProvider.mu.Unlock()
	commandFirewallProvider.fn = fn
}

func commandFirewallForAgent(agentID string) CommandFirewall {
	commandFirewallProvider.mu.RLock()
	fn := commandFirewallProvider.fn
	commandFirewallProvider.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(agentID)
}

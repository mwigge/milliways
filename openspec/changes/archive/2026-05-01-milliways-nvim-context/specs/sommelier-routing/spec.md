## MODIFIED Requirements

### Requirement: Pantry signals tier accepts editor context
The sommelier's pantry-signals tier SHALL accept an optional `editorcontext.Bundle` as an additional input and derive routing signals from it when present. Existing keyword, pantry, and learned-history tiers SHALL be unaffected when no bundle is present.

#### Scenario: Editor context signals derived
- **WHEN** a dispatch includes an editor context bundle with LSP diagnostics, cursor scope, git state, and project language
- **THEN** the sommelier SHALL derive `editor.lsp_error_count`, `editor.in_test_file`, `editor.dirty_churn`, and `editor.language` signals from the bundle

#### Scenario: No bundle — no signal
- **WHEN** a dispatch includes no editor context bundle
- **THEN** all `editor.*` signals SHALL be absent and routing SHALL behave identically to the pre-L2 baseline

#### Scenario: Kitchen weight_on editor signals in carte.yaml
- **WHEN** `carte.yaml` specifies `weight_on: { editor.lsp_error_count_gt_3: +0.15 }` for a kitchen
- **THEN** the sommelier SHALL apply that weight delta to the kitchen's routing score when the signal is true

#### Scenario: in_test_file signal derivation
- **WHEN** the cursor scope name starts with `test_` or `spec_`, or the buffer path ends with `_test.go` or `_spec.rb`
- **THEN** `editor.in_test_file` SHALL be true

#### Scenario: Missing bundle field treated as absent signal
- **WHEN** the bundle is present but a specific field (e.g., `git`) is null
- **THEN** signals derived from that field SHALL be absent, not zero, and SHALL not bias routing

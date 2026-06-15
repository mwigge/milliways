## ADDED Requirements

### Requirement: Qualified agentic clients own tiered coding plans
The system SHALL allow any Milliways agentic client that advertises the required planning, delegation, review, and continuation capabilities to own task decomposition, coordination, review, and final acceptance for tiered coding workflows.

#### Scenario: Supervisory agent delegates a bounded implementation unit
- **WHEN** the active qualified agent identifies a low-risk implementation unit suitable for local execution
- **THEN** the system creates a structured execution envelope and delegates only that unit to a qualified local executor

#### Scenario: Ambiguous work remains with the supervisory agent
- **WHEN** a task requires architectural decisions, unclear requirement interpretation, or unbounded cross-repository changes
- **THEN** the system SHALL keep the task with the supervisory agent and SHALL NOT delegate it to a local specialist

#### Scenario: Client lacks supervisory capabilities
- **WHEN** the active agentic client cannot emit or review the versioned execution envelope
- **THEN** the system SHALL keep the session in direct-run mode or select an explicitly configured qualified supervisor without silently changing clients

### Requirement: Delegated work uses a structured execution envelope
The system SHALL provide each local execution unit with a versioned envelope containing task identity, base revision, language, objective, allowed paths, forbidden paths, acceptance commands, resource budgets, and a required structured response schema.

#### Scenario: Executor changes an undeclared path
- **WHEN** a local executor produces an edit outside the allowed path set
- **THEN** the system SHALL reject the execution unit, preserve evidence of the violation, and return control to the supervisory agent

#### Scenario: Base revision changes before application
- **WHEN** the repository base revision no longer matches the envelope before edits are applied
- **THEN** the system SHALL reject the stale result and require the supervisory agent to re-plan or regenerate the unit

### Requirement: Deterministic verification gates local results
The system SHALL verify local execution results using repository state checks and configured formatters, compilers, tests, linters, and security gates rather than trusting model-reported success.

#### Scenario: Model claims success but a command fails
- **WHEN** a local model reports completion and any required acceptance command exits unsuccessfully
- **THEN** the system SHALL mark the unit failed and provide the captured failure evidence to the supervisory agent

#### Scenario: All checks and review pass
- **WHEN** the diff stays within scope, all required commands pass, and the supervisory agent accepts the result
- **THEN** the system SHALL mark the unit verified and continue the parent plan

### Requirement: Retry and escalation are bounded
The system SHALL enforce configurable retry, token, changed-file, and wall-time limits for local execution and SHALL escalate exhausted or unsafe work to the supervisory agent.

#### Scenario: Local retry budget is exhausted
- **WHEN** a local unit fails verification after its configured retry budget
- **THEN** the system SHALL stop local retries and route the unit to the supervisory agent with the complete attempt evidence

### Requirement: Tiered execution is observable and reversible
The system SHALL record planning, routing, execution, verification, fallback, and takeover events and SHALL provide a configuration switch that disables local execution.

#### Scenario: Operator disables local execution
- **WHEN** the local execution tier is disabled
- **THEN** queued local work SHALL drain or cancel safely and subsequent coding work SHALL remain with the active supervisory client

### Requirement: Tiered work is resumable
The system SHALL persist the state and evidence of plan, qualification, prewarm, execution, verification, review, retry, reroute, and takeover nodes.

#### Scenario: Daemon restarts after verification
- **WHEN** the daemon restarts after a unit has passed verification but before supervisory review
- **THEN** the workflow SHALL resume at review without repeating the local edit or acceptance commands unless repository state changed

### Requirement: Parallel execution prevents path conflicts
The system SHALL run local units in parallel only when their declared write scopes do not overlap and their verification operations can safely coexist.

#### Scenario: Two units target the same file
- **WHEN** two planned units include the same allowed write path
- **THEN** the orchestrator SHALL serialize them or return them to the supervisory agent for re-planning

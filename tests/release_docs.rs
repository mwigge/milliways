const README: &str = include_str!("../README.md");
const POLICY_RUNBOOK: &str = include_str!("../examples/policy-operations-runbook.md");

#[test]
fn external_database_support_is_documented_as_planned_contract() {
    assert_contains_all(
        README,
        &[
            "External Postgres support is planned as a contracted server deployment option",
            "SQLite remains the local default",
            "Postgres deployment contract will preserve the same rollup tiers",
        ],
    );
    assert_contains_all(
        POLICY_RUNBOOK,
        &[
            "External Postgres support is planned",
            "contracted server deployment option",
            "must preserve SQLite-compatible metric rollups and policy audit semantics",
        ],
    );
}

#[test]
fn router_maturity_controls_are_documented() {
    assert_contains_all(
        README,
        &[
            "Router maturity controls",
            "admission and backpressure",
            "timeout budgets",
            "non-secret failure responses",
        ],
    );
    assert_contains_all(
        POLICY_RUNBOOK,
        &[
            "admission and backpressure",
            "timeout budgets",
            "non-secret failure responses",
        ],
    );
}

#[test]
fn governance_contract_exports_are_documented() {
    assert_contains_all(
        README,
        &[
            "AQE/OpenAI-compatible governance contract exports",
            "policy decisions",
            "router maturity posture",
            "CRA evidence",
        ],
    );
    assert_contains_all(
        POLICY_RUNBOOK,
        &[
            "AQE/OpenAI-compatible governance contract exports",
            "policy decisions",
            "scanner posture",
            "CRA evidence",
        ],
    );
}

#[test]
fn chat_shortcut_documentation_matches_current_runner_count() {
    assert_contains_all(
        README,
        &[
            "/1 berget",
            "/2 minimax",
            "/3 claude",
            "/6 kimi",
            "/7 deepseek",
            "/9 local",
            "/10 pool",
            "`/1` … `/10`",
        ],
    );
}

#[test]
fn runner_and_loop_contracts_are_current() {
    assert_contains_all(
        README,
        &[
            "Berget, MiniMax, Claude, Codex, Pool, Gemini, Copilot, Kimi, DeepSeek, and local models",
            "Agentic loop turn cap",
            "| **Agentic loop turn cap** | 100 | `MILLIWAYS_MAX_TURNS=<n>` |",
            "Download, register for llama-swap, and activate/update the current local server when possible",
        ],
    );
    assert!(
        !README.contains("| **Agentic loop turn cap** | 10 |"),
        "README must not document the stale 10-turn HTTP loop cap"
    );
}

#[test]
fn policy_runbook_stays_passive_without_service_lifecycle_commands() {
    for forbidden in [
        "systemctl start ",
        "systemctl stop ",
        "systemctl restart ",
        "service milliways start",
        "service milliways stop",
        "service milliways restart",
        "brew services start",
        "brew services stop",
        "brew services restart",
    ] {
        assert!(
            !POLICY_RUNBOOK.contains(forbidden),
            "policy operations runbook must stay passive; found `{}`",
            forbidden
        );
    }
}

fn assert_contains_all(haystack: &str, needles: &[&str]) {
    for needle in needles {
        assert!(
            haystack.contains(needle),
            "expected documentation to contain `{}`",
            needle
        );
    }
}

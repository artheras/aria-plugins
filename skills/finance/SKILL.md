---
name: finance
description: Analyze financial statements, disclosed annual SEC facts, supplied equity series and backtests using the shared Arthera Financial Service.
---

Use the connected Financial Service tools; names may have the host's MCP prefix.
If tools are absent, explain that the service must be installed and configured. Installing this skill alone does not install or authorize a service. Setup is in the plugin README.

- financial_service_info reports the running version and host-selected network setting.
- financial_analyze_statements accepts supplied income_statement/balance_sheet/cashflow. A ticker can use Yahoo/SEC only with host-enabled network access. Results use the existing success/data/source_attempts contract.
- financial_parse_sec_facts parses caller-supplied companyfacts and preserves one aligned annual filing.
- financial_risk_metrics accepts ordered positive equity values and an annual observation frequency; state that assumption and the zero risk-free-rate convention.
- financial_backtest simulates supplied dated OHLC bars; buy_hold and sma_cross use next-open fills. It places no orders. Returned series/trades are limited to their last 100 entries.
- financial_market_history retrieves unadjusted daily bars, not a live quote or total-return series, only when the host enables network access.

Report source, reporting period/as_of, assumptions, missing values and returned limitations. Preserve source failures and stale-disclosure labels. Do not present caller-supplied or synthetic data as independently verified market data. Use comparable periods and currencies; ask for missing context when it affects a conclusion.

Treat success=false and MCP errors as failed operations. Do not fill missing figures or describe unavailable solvency classifications as computed results. Use other available evidence with explicit gaps.

Network permission is host configuration, not a tool argument. Explain the setting when needed; do not change it or bypass a disabled source through another channel without the user's authorization. This plugin has no trading, broker-confirmation or publishing tools. Producing analysis does not authorize those actions.

For broker questions, financial_broker_connections lists only this host user's configured connections. Use financial_broker_account, financial_broker_positions and financial_broker_orders for the requested connection ID. Do not request API keys, passwords or OAuth tokens in chat. If access is disabled, direct the user to the Financial Service broker connection guide and host settings. Never alter permission settings or retrieve another user's profile to complete a query. A simulation label must remain explicit; an unknown simulation status must not be called live. Broker retrieval time does not establish quote freshness. No trading or cancellation tools are available.

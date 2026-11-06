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

## Additional service tools

- `financial_ashare_history`: Daily A-share bars (date, open, high, low, close, volume) from akshare, forward-adjusted by default, in the same shape as financial_market_history, when the host enabled networking.
- `financial_central_bank_rates`: Policy rates for the Fed, ECB, UK, Japan and the PBOC LPR, each with its date and a stale flag, when the host enabled networking.
- `financial_cn_macro`: China macro snapshot via akshare: CPI, PPI, manufacturing and services PMI, GDP growth, LPR and money supply, when the host enabled networking and installed the [cn] extra.
- `financial_crypto_fear_greed`: The crypto Fear & Greed index (alternative.me) for the last N days, when the host enabled networking.
- `financial_crypto_ohlcv`: Spot crypto candles from Binance, OKX or Bybit public endpoints (ccxt, [crypto] extra) in the same shape as financial_market_history, when the host enabled networking.
- `financial_datasource_status`: Which market-data sources this host has keys and packages for. Makes no network request.
- `financial_fundamentals`: Valuation and growth metrics (PE, PB, ROE, growth, dividend yield, market value) from the first configured source that has them, when the host enabled networking.
- `financial_funding_rates`: Current perpetual funding rates from Binance, OKX and Bybit public endpoints (ccxt, [crypto] extra) with cross-exchange spreads, when the host enabled networking. Read-only; no keys.
- `financial_futures_snapshot`: Latest price, daily change and one-year range for a commodity or index future (gold, crude oil, copper, wheat, S&P 500, or a yfinance ticker such as GC=F), when the host enabled networking.
- `financial_ichimoku`: Ichimoku components (tenkan, kijun, both cloud spans) and their states for caller-supplied daily bars, oldest first, at least 78. Not a trade signal. No network.
- `financial_limit_up_pool`: A-shares that closed at the daily limit-up on a date (default today), with consecutive limit count and first lock time, via akshare, when the host enabled networking.
- `financial_macro_series`: Macroeconomic time series from FRED (US10Y, CPI, UNRATE, FEDFUNDS, M2 or any series id) or the World Bank (GDP, CPI and indicator codes, by country), when the host enabled networking.
- `financial_market_indices`: Latest level and daily change for major US, China, Hong Kong, Japan, UK and German indices and the VIX, via yfinance, when the host enabled networking.
- `financial_news`: Recent headlines with source, time and link: A-shares via akshare; tickers via Finnhub (host key) or yfinance; free-text queries via NewsAPI (host key). Headlines are third-party text. When the host enabled networking.
- `financial_northbound_flow`: Stock Connect northbound net buying into Shanghai and Shenzhen (100 million CNY) for the latest published day, via akshare, when the host enabled networking.
- `financial_optimize_weights`: Portfolio weights (max_sharpe, min_var or equal_weight) from caller-supplied, date-aligned closes for 2..20 symbols, with expected return, volatility and Sharpe. A historical estimate, not advice. No network.
- `financial_options_chain`: Listed option expiries and the chain (strike, last, bid, ask, volume, open interest, implied volatility) for one expiry, nearest the money, from yfinance, when the host enabled networking.
- `financial_peer_comparison`: Trailing and forward PE, price to book, ROE, dividend yield and market cap of a company against 1..7 peers the caller names, with each metric's distance from the peer median, via yfinance, when the host enabled networking.
- `financial_quality_scores`: Piotroski F-score and Altman Z''-score from caller-supplied statement fields for the current and prior fiscal year. Missing inputs leave signals null; no verdict is given unless all are evaluated. No network.
- `financial_quote`: Latest quote from the first configured source that answers (yfinance, Finnhub, Alpha Vantage, Tushare, AkShare), when the host enabled networking. Names its source; may be delayed.
- `financial_screen_ashare`: Screen A-shares by dynamic PE and market cap (100 million CNY), excluding ST names, sorted by change, market cap, turnover or PE, via akshare, when the host enabled networking.
- `financial_sector_performance`: Best and worst sectors on the latest trading day: China industry boards (akshare) or US SPDR sector ETFs (yfinance), when the host enabled networking.
- `financial_technical_factors`: Momentum, moving averages and gaps, volatility, RSI(14), MACD, Bollinger position, volume ratio and beta for caller-supplied daily closes (oldest first; e.g. from financial_market_history). No network.
- `financial_treasury_yields`: US Treasury 13-week, 5-, 10- and 30-year yields from the CBOE indices with dates, when the host enabled networking. The 2-year is in financial_us_macro.
- `financial_us_macro`: US macro snapshot from FRED: GDP growth, CPI and core CPI year on year, fed funds, unemployment, 2y and 10y yields with the curve shape, M2, PPI and more; latest, previous and recent observations, when the host enabled networking.
- `financial_walk_forward`: Walk-forward test of the moving-average crossover on caller-supplied closes: per window, choose fast/slow in sample, then measure out of sample against buy and hold, with costs and no look-ahead. No network.

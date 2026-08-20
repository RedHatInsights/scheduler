# Payload Templating

The scheduler supports dynamic payload values using [Google CEL (Common Expression Language)](https://cel.dev/) expressions. Any string value in a job payload prefixed with `scheduler_cel:` is treated as a template expression that gets evaluated at execution time. Non-prefixed strings pass through unchanged.

## Table of Contents

1. [Overview](#overview)
2. [Context Variables](#context-variables)
3. [Date Functions](#date-functions)
4. [Format Constants](#format-constants)
5. [Composability](#composability)
6. [Examples](#examples)
7. [Validation](#validation)
8. [Security Limits](#security-limits)

---

## Overview

Scheduled jobs often need date-relative parameters. A monthly export job should request "last month's data" relative to when it runs, not a hardcoded date range. Payload templating solves this by evaluating `scheduler_cel:` expressions at execution time.

**Static payload (no templating):**
```json
{
  "filters": {
    "start_date": "2026-07-01",
    "end_date": "2026-07-31"
  }
}
```

**Templated payload (evaluated every run):**
```json
{
  "filters": {
    "start_date": "scheduler_cel:now.first_of_last_month().format_date(ISO_DATE)",
    "end_date": "scheduler_cel:now.last_of_last_month().format_date(ISO_DATE)"
  }
}
```

When this job runs on August 18, 2026, the payload resolves to:
```json
{
  "filters": {
    "start_date": "2026-07-01",
    "end_date": "2026-07-31"
  }
}
```

When it runs again on September 5, 2026, it resolves to:
```json
{
  "filters": {
    "start_date": "2026-08-01",
    "end_date": "2026-08-31"
  }
}
```

---

## Context Variables

These variables are available in every CEL expression:

| Variable | Type | Description |
|----------|------|-------------|
| `now` | `timestamp` | Current UTC time at the moment of job execution |
| `job_id` | `string` | The UUID of the job being executed |

---

## Date Functions

All date functions are member functions on `timestamp` values. They return `timestamp` unless noted otherwise, so they can be chained.

### Day Operations

| Function | Return Type | Description |
|----------|-------------|-------------|
| `ts.start_of_day()` | `timestamp` | Midnight (00:00:00) UTC of the same day |
| `ts.end_of_day()` | `timestamp` | End of day (23:59:59) UTC of the same day |
| `ts.add_days(n)` | `timestamp` | Add `n` calendar days (negative to subtract) |

### Month Operations

| Function | Return Type | Description |
|----------|-------------|-------------|
| `ts.first_of_month()` | `timestamp` | 1st of the current month at 00:00:00 UTC |
| `ts.last_of_month()` | `timestamp` | Last day of the current month at 00:00:00 UTC |
| `ts.first_of_last_month()` | `timestamp` | 1st of the previous month at 00:00:00 UTC |
| `ts.last_of_last_month()` | `timestamp` | Last day of the previous month at 00:00:00 UTC |
| `ts.add_months(n)` | `timestamp` | Add `n` calendar months (negative to subtract). Days are clamped to the last day of the target month (e.g. Jan 31 + 1 month = Feb 28) |

### Week Operations (ISO 8601, Monday-based)

| Function | Return Type | Description |
|----------|-------------|-------------|
| `ts.first_of_week()` | `timestamp` | Monday of the current week at 00:00:00 UTC |
| `ts.last_of_week()` | `timestamp` | Sunday of the current week at 00:00:00 UTC |

### Quarter Operations

| Function | Return Type | Description |
|----------|-------------|-------------|
| `ts.first_of_quarter()` | `timestamp` | 1st day of the current quarter at 00:00:00 UTC |
| `ts.last_of_quarter()` | `timestamp` | Last day of the current quarter at 00:00:00 UTC |

Quarters: Q1 = Jan-Mar, Q2 = Apr-Jun, Q3 = Jul-Sep, Q4 = Oct-Dec.

### Formatting

| Function | Return Type | Description |
|----------|-------------|-------------|
| `ts.format_date(layout)` | `string` | Format the timestamp using a Go layout string or a named constant |

---

## Format Constants

Named constants for common date formats. Use these instead of raw Go layout strings.

| Constant | Go Layout | Example Output |
|----------|-----------|----------------|
| `ISO_DATE` | `2006-01-02` | `2026-08-18` |
| `ISO_DATETIME` | `2006-01-02T15:04:05Z` | `2026-08-18T14:30:45Z` |
| `ISO_8601` | `2006-01-02T15:04:05Z07:00` | `2026-08-18T14:30:45Z` |
| `US_DATE` | `01/02/2006` | `08/18/2026` |
| `EU_DATE` | `02/01/2006` | `18/08/2026` |
| `DATE_SLASH` | `2006/01/02` | `2026/08/12` |
| `YEAR_MONTH` | `2006-01` | `2026-08` |
| `MONTH_DAY` | `01-02` | `08-18` |
| `DATETIME_FULL` | `2006-01-02 15:04:05` | `2026-08-18 14:30:45` |

You can also pass a raw Go layout string directly: `format_date('Jan 2, 2006')`.

---

## Composability

All timestamp functions return timestamps, so they chain naturally:

```
now.first_of_month().add_days(-1).format_date(ISO_DATE)
```

This evaluates as: current month's 1st → subtract 1 day → last day of previous month → format.

More examples:

```
now.add_months(-3).first_of_month().format_date(ISO_DATE)
```
Three months ago, first of that month.

```
now.last_of_quarter().end_of_day().format_date(ISO_DATETIME)
```
Last day of the current quarter at 23:59:59Z.

```
now.first_of_week().start_of_day().format_date(ISO_DATETIME)
```
Monday of the current week at 00:00:00Z.

---

## Examples

### Subscriptions Export (last month)

The rhsm-subscriptions service expects `beginning` and `ending` fields in ISO 8601 datetime format with UTC offset (`YYYY-MM-DDTHH:MM:SSZ`).

```json
{
  "name": "swatch-instances-export",
  "format": "json",
  "sources": [
    {
      "application": "subscriptions",
      "resource": "instances",
      "filters": {
        "product_id": "rhel-for-x86-els-payg",
        "beginning": "scheduler_cel:now.first_of_last_month().start_of_day().format_date(ISO_DATETIME)",
        "ending": "scheduler_cel:now.last_of_last_month().end_of_day().format_date(ISO_DATETIME)"
      }
    }
  ]
}
```

On August 18, 2026 this resolves to:
```json
{
  "filters": {
    "product_id": "rhel-for-x86-els-payg",
    "beginning": "2026-07-01T00:00:00Z",
    "ending": "2026-07-31T23:59:59Z"
  }
}
```

### Month-to-date Export

```json
{
  "name": "mtd-inventory-export",
  "format": "csv",
  "sources": [
    {
      "application": "inventory",
      "resource": "hosts",
      "filters": {
        "start_date": "scheduler_cel:now.first_of_month().format_date(ISO_DATE)",
        "end_date": "scheduler_cel:now.format_date(ISO_DATE)"
      }
    }
  ]
}
```

### Rolling 7-Day Window

```json
{
  "filters": {
    "since": "scheduler_cel:now.add_days(-7).start_of_day().format_date(ISO_DATETIME)",
    "until": "scheduler_cel:now.end_of_day().format_date(ISO_DATETIME)"
  }
}
```

### Quarterly Report

```json
{
  "filters": {
    "quarter_begin": "scheduler_cel:now.first_of_quarter().format_date(ISO_DATE)",
    "quarter_end": "scheduler_cel:now.last_of_quarter().format_date(ISO_DATE)"
  }
}
```

### Using job_id

```json
{
  "reference": "scheduler_cel:job_id",
  "tag": "scheduler_cel:job_id + '_monthly_export'"
}
```

### Mixed Static and Templated Fields

Non-`scheduler_cel:` values pass through unchanged. Only string values with the `scheduler_cel:` prefix are evaluated.

```json
{
  "name": "monthly-export",
  "format": "json",
  "sources": [
    {
      "application": "subscriptions",
      "resource": "instances",
      "filters": {
        "product_id": "rhel-for-x86-els-payg",
        "beginning": "scheduler_cel:now.first_of_last_month().format_date(ISO_DATETIME)",
        "ending": "scheduler_cel:now.last_of_last_month().end_of_day().format_date(ISO_DATETIME)"
      }
    }
  ]
}
```

Numbers, booleans, and objects without `scheduler_cel:` strings are left as-is.

---

## Validation

CEL expressions are validated at two points:

### API Time (Create / Update / Patch)

When a job is created or modified, all `scheduler_cel:` expressions in the payload are **compiled but not evaluated**. This catches syntax errors immediately and returns a `400 Bad Request` with details:

```json
{
  "errors": [
    {
      "status": "400",
      "title": "Invalid Payload Template",
      "detail": "payload contains invalid CEL expression: field 'start_date': compile error: ..."
    }
  ]
}
```

Payloads with no `scheduler_cel:` expressions skip validation entirely.

### Execution Time

When the job runs, all `scheduler_cel:` expressions are evaluated with the current `now` timestamp and `job_id`. If evaluation fails at runtime (e.g., due to a cost limit exceeded), the job execution fails and the error is recorded in the job run history.

---

## Security Limits

The CEL evaluator enforces four limits to prevent abuse:

| Limit | Value | Purpose |
|-------|-------|---------|
| Max expression length | 1,024 characters | Prevents oversized expressions |
| Max evaluation cost | 10,000 | CEL's built-in cost model kills expensive comprehensions |
| Max payload nesting depth | 20 levels | Prevents stack overflow from deeply nested structures |
| Max CEL expressions per payload | 50 | Caps total number of evaluated expressions |

These limits apply to both validation (API time) and evaluation (execution time).

The CEL environment is sandboxed: expressions can only access the declared context variables (`now`, `job_id`) and the provided date functions. There is no access to the filesystem, network, environment variables, or any other system resources.

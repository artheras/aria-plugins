terraform {
  required_providers {
    google = { source = "hashicorp/google", version = ">= 6.0, < 8.0" }
  }
}

variable "project_id" { type = string }
variable "database_id" { type = string }
variable "service_name" { type = string
  default = "aria-commerce" }
variable "recovery_job" { type = string
  default = "aria-commerce-reconcile" }
variable "notification_channels" {
  type = list(string)
  default = []
  description = "Existing Monitoring channel resource names. No recipient is created automatically."
}

resource "google_firestore_backup_schedule" "commerce_daily" {
  project = var.project_id
  database = var.database_id
  retention = "604800s"
  daily_recurrence {}
}

resource "google_monitoring_alert_policy" "commerce_failures" {
  project = var.project_id
  display_name = "Commerce service errors"
  combiner = "OR"
  conditions {
    display_name = "Commerce HTTP 5xx"
    condition_matched_log {
      filter = "resource.type=\"cloud_run_revision\" resource.labels.service_name=\"${var.service_name}\" jsonPayload.message=\"http_request\" jsonPayload.status>=500"
    }
  }
  alert_strategy {
    notification_rate_limit { period = "300s" }
    auto_close = "1800s"
  }
  notification_channels = var.notification_channels
}

resource "google_monitoring_alert_policy" "commerce_recovery" {
  project = var.project_id
  display_name = "Commerce recovery needs attention"
  combiner = "OR"
  conditions {
    display_name = "Recovery failure or merchant review"
    condition_matched_log {
      filter = "resource.type=\"cloud_run_job\" resource.labels.job_name=\"${var.recovery_job}\" jsonPayload.message=(\"recovery_failed\" OR \"recovery_scan_limit\" OR \"recovery_requires_review\" OR \"refund_requires_review\")"
    }
  }
  alert_strategy {
    notification_rate_limit { period = "300s" }
    auto_close = "1800s"
  }
  notification_channels = var.notification_channels
}

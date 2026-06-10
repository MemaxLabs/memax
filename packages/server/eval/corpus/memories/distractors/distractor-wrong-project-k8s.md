## Kubernetes Deployment — ExampleCorp Billing Service

Production Kubernetes setup for the billing microservice at ExampleCorp. Cluster runs on GKE (us-central1). Key configuration:

- **Namespace:** `billing-prod`
- **Replicas:** 5 pods with HPA scaling to 20 based on CPU (target 65%) and memory (target 75%)
- **Helm chart:** `infra/k8s/billing/Chart.yaml`, values override in `values-prod.yaml`
- **Database:** Cloud SQL PostgreSQL with PgBouncer sidecar (max 100 connections per pod)
- **Secrets:** Managed via Google Secret Manager, mounted as env vars through ExternalSecrets operator

Deploy command: `helm upgrade --install billing-prod ./infra/k8s/billing -f values-prod.yaml --namespace billing-prod`

Monitoring: Datadog APM traces + custom billing metrics dashboard. Alert on p99 latency > 500ms or error rate > 1%.

Rollback: `helm rollback billing-prod 1 --namespace billing-prod`. Always verify the payment processing queue is drained before rolling back.

# MySQL User Operator

This Kubernetes operator manages MySQL users in a MySQL database through custom resources. The operator automates the process of creating users, setting permissions, and managing database access without manual intervention.

## Features

- Automatic MySQL user creation based on custom resources
- Database creation if it doesn't exist
- Password management through Kubernetes secrets
- Fine-grained permission control
- Resource quotas for users (max connections, queries per hour, etc.)
- Automatic cleanup when resources are deleted

## Installation

### Prerequisites

- Kubernetes cluster 1.19+
- kubectl configured to communicate with your cluster
- Access to a MySQL database

### Deploy the Custom Resource Definition

```bash
kubectl apply -f config/crd/mysql_v1_mysqluser.yaml
```

### Create the operator namespace

```bash
kubectl create namespace mysql-operator-system
```

### Deploy RBAC components

```bash
kubectl apply -f config/rbac.yaml
```

### Deploy the operator

Build and push the Docker image:

```bash
docker build -t your-registry/mysql-operator:v0.1.0 .
docker push your-registry/mysql-operator:v0.1.0
```

Update the image reference in the deployment.yaml file and deploy:

```bash
# Update the image reference
sed -i 's|${REGISTRY}|your-registry|g' config/deployment.yaml
sed -i 's|${TAG}|v0.1.0|g' config/deployment.yaml

# Deploy the operator
kubectl apply -f config/deployment.yaml
```

## Usage

### Creating a MySQL admin secret

Before creating MySQL users, you need to create a secret with MySQL admin credentials:

```bash
kubectl create secret generic mysql-admin-credentials \
  --from-literal=host=mysql.example.com \
  --from-literal=port=3306 \
  --from-literal=username=admin \
  --from-literal=password=admin-password
```

### Creating a MySQL user

Create a MySQLUser custom resource:

```yaml
apiVersion: mysql.example.com/v1
kind: MySQLUser
metadata:
  name: app-user
spec:
  username: appuser
  database: appdb
  permissions:
    - SELECT
    - INSERT
    - UPDATE
    - DELETE
  databaseRef:
    secretName: mysql-admin-credentials
    namespace: default
  resourceQuota:
    maxConnections: 5
    maxQueriesPerHour: 1000
```

Apply it to your cluster:

```bash
kubectl apply -f your-mysqluser.yaml
```

### Accessing user credentials

The operator creates a secret containing the user credentials:

```bash
kubectl get secret app-user-mysql-credentials -o jsonpath='{.data.password}' | base64 --decode
```

## Custom Resource Reference

### MySQLUser

| Field | Type | Description |
|-------|------|-------------|
| spec.username | string | Name of the MySQL user to create |
| spec.database | string | Name of the database to create and grant access to |
| spec.permissions | []string | List of MySQL permissions (e.g., SELECT, INSERT) |
| spec.databaseRef | object | Reference to MySQL instance |
| spec.databaseRef.secretName | string | Name of secret with MySQL admin credentials |
| spec.databaseRef.namespace | string | Namespace of the secret (optional) |
| spec.secretName | string | Name for the generated user secret (optional) |
| spec.resourceQuota | object | Resource limits for the user (optional) |

## Architecture

The MySQL User Operator follows the Kubernetes Operator pattern:

1. Watches for MySQLUser custom resources
2. Connects to MySQL using admin credentials
3. Creates/updates users, databases, and permissions
4. Stores user credentials in Kubernetes secrets
5. Updates status of resources to reflect current state

## Security Considerations

- The operator needs admin access to MySQL to create users and databases
- User passwords are stored in Kubernetes secrets
- Admin credentials must be protected
- Use Kubernetes RBAC to control who can create MySQLUser resources

## Troubleshooting

Check operator logs:

```bash
kubectl logs -n mysql-operator-system deployment/mysql-operator
```

Check resource status:

```bash
kubectl get mysqluser app-user -o yaml
```
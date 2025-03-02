package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MySQLUserSpec defines the desired state of MySQLUser
type MySQLUserSpec struct {
	// Username is the name of the MySQL user to create
	Username string `json:"username"`

	// Database is the name of the database the user will have access to
	Database string `json:"database"`

	// Permissions defines the permissions for the user (e.g. SELECT, INSERT, etc.)
	// +optional
	Permissions []string `json:"permissions,omitempty"`

	// DatabaseRef is a reference to a MySQL instance
	DatabaseRef MySQLDatabaseRef `json:"databaseRef"`

	// SecretName is the name of the secret containing the user's password
	// If not provided, a password will be auto-generated and stored in a new secret
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// ResourceQuota allows setting limits on the user's resource usage
	// +optional
	ResourceQuota *ResourceQuota `json:"resourceQuota,omitempty"`
}

// MySQLDatabaseRef contains the information necessary to connect to a MySQL instance
type MySQLDatabaseRef struct {
	// Name of the secret containing the MySQL connection information
	SecretName string `json:"secretName"`

	// Namespace where the secret is located, defaults to the same namespace as the MySQLUser
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Keys in the secret for host, port, username and password
	// +optional
	HostKey string `json:"hostKey,omitempty"`
	// +optional
	PortKey string `json:"portKey,omitempty"`
	// +optional
	UsernameKey string `json:"usernameKey,omitempty"`
	// +optional
	PasswordKey string `json:"passwordKey,omitempty"`
}

// ResourceQuota defines the resource limits for a MySQL user
type ResourceQuota struct {
	// MaxConnections is the maximum number of concurrent connections allowed
	// +optional
	MaxConnections *int32 `json:"maxConnections,omitempty"`

	// MaxQueriesPerHour limits the number of queries per hour
	// +optional
	MaxQueriesPerHour *int32 `json:"maxQueriesPerHour,omitempty"`

	// MaxUpdatesPerHour limits the number of updates per hour
	// +optional
	MaxUpdatesPerHour *int32 `json:"maxUpdatesPerHour,omitempty"`

	// MaxConnectionsPerHour limits the number of connections per hour
	// +optional
	MaxConnectionsPerHour *int32 `json:"maxConnectionsPerHour,omitempty"`
}

// MySQLUserStatus defines the observed state of MySQLUser
type MySQLUserStatus struct {
	// Conditions represent the latest available observations of the MySQLUser state
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase represents the current phase of the MySQL user (Pending, Active, Failed)
	// +optional
	Phase string `json:"phase,omitempty"`

	// LastPasswordRotation is the timestamp of the last password rotation
	// +optional
	LastPasswordRotation *metav1.Time `json:"lastPasswordRotation,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Username",type="string",JSONPath=".spec.username"
//+kubebuilder:printcolumn:name="Database",type="string",JSONPath=".spec.database"
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// MySQLUser is the Schema for the mysqlusers API
type MySQLUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MySQLUserSpec   `json:"spec,omitempty"`
	Status MySQLUserStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MySQLUserList contains a list of MySQLUser
type MySQLUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MySQLUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MySQLUser{}, &MySQLUserList{})
}

package controllers

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-sql-driver/mysql"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mysqlv1 "github.com/example/mysql-operator/api/v1"
)

const (
	mysqlUserFinalizer = "mysql.example.com/finalizer"

	// Status phases
	PhaseCreating = "Creating"
	PhaseActive   = "Active"
	PhaseFailed   = "Failed"
)

// MySQLUserReconciler reconciles a MySQLUser object
type MySQLUserReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=mysql.example.com,resources=mysqlusers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mysql.example.com,resources=mysqlusers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mysql.example.com,resources=mysqlusers/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MySQLUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("mysqluser", req.NamespacedName)
	logger.Info("Starting reconciliation")

	// Fetch the MySQLUser instance
	mysqlUser := &mysqlv1.MySQLUser{}
	if err := r.Get(ctx, req.NamespacedName, mysqlUser); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted
			logger.Info("MySQLUser resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request
		logger.Error(err, "Failed to get MySQLUser")
		return ctrl.Result{}, err
	}

	// Initialize status if it's a new resource
	if mysqlUser.Status.Phase == "" {
		mysqlUser.Status.Phase = PhaseCreating
		if err := r.Status().Update(ctx, mysqlUser); err != nil {
			logger.Error(err, "Failed to update MySQLUser status")
			return ctrl.Result{}, err
		}
	}

	// Check if the MySQLUser is being deleted
	if !mysqlUser.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, mysqlUser, logger)
	}

	// Add finalizer if it doesn't exist
	if !controllerutil.ContainsFinalizer(mysqlUser, mysqlUserFinalizer) {
		controllerutil.AddFinalizer(mysqlUser, mysqlUserFinalizer)
		if err := r.Update(ctx, mysqlUser); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Get MySQL connection details
	adminConn, err := r.getMySQLAdminConnection(ctx, mysqlUser)
	if err != nil {
		r.updateStatus(ctx, mysqlUser, PhaseFailed, fmt.Sprintf("Failed to get MySQL connection: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}
	defer adminConn.Close()

	// Handle user password in a secret
	userSecret, err := r.ensureUserSecret(ctx, mysqlUser, logger)
	if err != nil {
		r.updateStatus(ctx, mysqlUser, PhaseFailed, fmt.Sprintf("Failed to manage user secret: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Get the password from the secret
	password := string(userSecret.Data["password"])

	// Create or update the MySQL user
	if err := r.ensureMySQLUser(ctx, adminConn, mysqlUser, password, logger); err != nil {
		r.updateStatus(ctx, mysqlUser, PhaseFailed, fmt.Sprintf("Failed to ensure MySQL user: %v", err))
		return ctrl.Result{RequeueAfter: time.Minute}, err
	}

	// Set status to active
	r.updateStatus(ctx, mysqlUser, PhaseActive, "User reconciled successfully")

	logger.Info("Reconciliation completed successfully")
	return ctrl.Result{RequeueAfter: time.Hour}, nil
}

func (r *MySQLUserReconciler) updateStatus(ctx context.Context, mysqlUser *mysqlv1.MySQLUser, phase, message string) error {
	mysqlUser.Status.Phase = phase

	// Update the condition
	meta.SetStatusCondition(&mysqlUser.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionStatus(phase == PhaseActive),
		LastTransitionTime: metav1.Now(),
		Reason:             "Reconciling",
		Message:            message,
	})

	mysqlUser.Status.ObservedGeneration = mysqlUser.Generation
	return r.Status().Update(ctx, mysqlUser)
}

func (r *MySQLUserReconciler) getMySQLAdminConnection(ctx context.Context, mysqlUser *mysqlv1.MySQLUser) (*sql.DB, error) {
	// Get the secret with MySQL admin credentials
	secretName := mysqlUser.Spec.DatabaseRef.SecretName
	secretNamespace := mysqlUser.Spec.DatabaseRef.Namespace
	if secretNamespace == "" {
		secretNamespace = mysqlUser.Namespace
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: secretNamespace}, secret); err != nil {
		return nil, fmt.Errorf("failed to get MySQL secret: %w", err)
	}

	// Get connection details from secret
	hostKey := mysqlUser.Spec.DatabaseRef.HostKey
	if hostKey == "" {
		hostKey = "host"
	}
	portKey := mysqlUser.Spec.DatabaseRef.PortKey
	if portKey == "" {
		portKey = "port"
	}
	usernameKey := mysqlUser.Spec.DatabaseRef.UsernameKey
	if usernameKey == "" {
		usernameKey = "username"
	}
	passwordKey := mysqlUser.Spec.DatabaseRef.PasswordKey
	if passwordKey == "" {
		passwordKey = "password"
	}

	host := string(secret.Data[hostKey])
	port := string(secret.Data[portKey])
	username := string(secret.Data[usernameKey])
	password := string(secret.Data[passwordKey])

	if host == "" || port == "" || username == "" || password == "" {
		return nil, fmt.Errorf("missing required MySQL connection details in secret %s", secretName)
	}

	// Configure MySQL connection
	config := mysql.NewConfig()
	config.User = username
	config.Passwd = password
	config.Net = "tcp"
	config.Addr = fmt.Sprintf("%s:%s", host, port)
	config.Params = map[string]string{
		"parseTime": "true",
	}

	// Open MySQL connection
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		return nil, err
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func (r *MySQLUserReconciler) ensureUserSecret(ctx context.Context, mysqlUser *mysqlv1.MySQLUser, logger logr.Logger) (*corev1.Secret, error) {
	secretName := mysqlUser.Spec.SecretName
	if secretName == "" {
		// Auto-generate a secret name if not provided
		secretName = fmt.Sprintf("%s-mysql-credentials", mysqlUser.Name)
	}

	// Check if secret exists
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: mysqlUser.Namespace}, secret)
	if err == nil {
		// Secret exists, use the stored password
		logger.Info("Using existing secret for MySQL user")
		return secret, nil
	}

	// Create new secret with generated password if it doesn't exist
	if errors.IsNotFound(err) {
		password := generatePassword(20)

		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: mysqlUser.Namespace,
				Labels: map[string]string{
					"app.kubernetes.io/managed-by": "mysql-operator",
					"mysql.example.com/user":       mysqlUser.Name,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"username": []byte(mysqlUser.Spec.Username),
				"password": []byte(password),
				"database": []byte(mysqlUser.Spec.Database),
			},
		}

		// Set owner reference to automatically delete secret when MySQLUser is deleted
		if err := controllerutil.SetControllerReference(mysqlUser, secret, r.Scheme); err != nil {
			return nil, err
		}

		if err := r.Create(ctx, secret); err != nil {
			return nil, err
		}

		logger.Info("Created new secret for MySQL user")
		return secret, nil
	}

	return nil, err
}

func (r *MySQLUserReconciler) ensureMySQLUser(ctx context.Context, db *sql.DB, mysqlUser *mysqlv1.MySQLUser, password string, logger logr.Logger) error {
	username := mysqlUser.Spec.Username
	database := mysqlUser.Spec.Database

	// Check if user exists
	var userExists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM mysql.user WHERE user = ?)", username).Scan(&userExists)
	if err != nil {
		return fmt.Errorf("failed to check if user exists: %w", err)
	}

	// Create or update user with password
	if !userExists {
		// Create user
		query := fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", username, password)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to create MySQL user: %w", err)
		}
		logger.Info("Created MySQL user", "username", username)
	} else {
		// Update user password
		query := fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s'", username, password)
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to update MySQL user password: %w", err)
		}
		logger.Info("Updated MySQL user password", "username", username)
	}

	// Create database if it doesn't exist
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", database)
	if _, err := db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	logger.Info("Ensured database exists", "database", database)

	// Grant permissions
	permissions := "ALL PRIVILEGES"
	if len(mysqlUser.Spec.Permissions) > 0 {
		permissions = strings.Join(mysqlUser.Spec.Permissions, ", ")
	}

	grantQuery := fmt.Sprintf("GRANT %s ON %s.* TO '%s'@'%%'", permissions, database, username)
	if _, err := db.ExecContext(ctx, grantQuery); err != nil {
		return fmt.Errorf("failed to grant permissions: %w", err)
	}
	logger.Info("Granted permissions to user", "username", username, "permissions", permissions)

	// Apply resource quota if specified
	if quota := mysqlUser.Spec.ResourceQuota; quota != nil {
		if err := r.applyResourceQuota(ctx, db, mysqlUser, username); err != nil {
			return err
		}
	}

	// Apply changes
	if _, err := db.ExecContext(ctx, "FLUSH PRIVILEGES"); err != nil {
		return fmt.Errorf("failed to flush privileges: %w", err)
	}

	return nil
}

func (r *MySQLUserReconciler) applyResourceQuota(ctx context.Context, db *sql.DB, mysqlUser *mysqlv1.MySQLUser, username string) error {
	quota := mysqlUser.Spec.ResourceQuota

	// Build resource limits
	var limits []string

	if quota.MaxConnections != nil {
		limits = append(limits, fmt.Sprintf("MAX_USER_CONNECTIONS %d", *quota.MaxConnections))
	}
	if quota.MaxQueriesPerHour != nil {
		limits = append(limits, fmt.Sprintf("MAX_QUERIES_PER_HOUR %d", *quota.MaxQueriesPerHour))
	}
	if quota.MaxUpdatesPerHour != nil {
		limits = append(limits, fmt.Sprintf("MAX_UPDATES_PER_HOUR %d", *quota.MaxUpdatesPerHour))
	}
	if quota.MaxConnectionsPerHour != nil {
		limits = append(limits, fmt.Sprintf("MAX_CONNECTIONS_PER_HOUR %d", *quota.MaxConnectionsPerHour))
	}

	if len(limits) > 0 {
		query := fmt.Sprintf("ALTER USER '%s'@'%%' WITH %s", username, strings.Join(limits, " "))
		if _, err := db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to apply resource quota: %w", err)
		}
	}

	return nil
}

func (r *MySQLUserReconciler) handleDeletion(ctx context.Context, mysqlUser *mysqlv1.MySQLUser, logger logr.Logger) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(mysqlUser, mysqlUserFinalizer) {
		return ctrl.Result{}, nil
	}

	logger.Info("Handling deletion of MySQL user")

	// Connect to MySQL
	db, err := r.getMySQLAdminConnection(ctx, mysqlUser)
	if err != nil {
		// If we can't connect to MySQL, we still want to remove finalizer to allow deletion
		logger.Error(err, "Failed to connect to MySQL during deletion, removing finalizer anyway")
	} else {
		defer db.Close()

		// Drop user if it exists
		_, err = db.ExecContext(ctx, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", mysqlUser.Spec.Username))
		if err != nil {
			logger.Error(err, "Failed to drop MySQL user", "username", mysqlUser.Spec.Username)
			// Continue with finalizer removal despite error
		} else {
			logger.Info("MySQL user dropped successfully", "username", mysqlUser.Spec.Username)
		}
	}

	// Remove finalizer to allow deletion
	controllerutil.RemoveFinalizer(mysqlUser, mysqlUserFinalizer)
	if err := r.Update(ctx, mysqlUser); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Removed finalizer from MySQLUser")
	return ctrl.Result{}, nil
}

func generatePassword(length int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()-_=+[]{}|;:,.<>?"
	password := make([]byte, length)
	for i := range password {
		password[i] = chars[r.Intn(len(chars))]
	}
	return string(password)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MySQLUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mysqlv1.MySQLUser{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}

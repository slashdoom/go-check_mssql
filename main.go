package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"example.org/config"
	"example.org/logger"

	_ "github.com/denisenkom/go-mssqldb"

	"github.com/spf13/pflag"
	"go.uber.org/zap"
)

const VERSION string = "0.0.1"

// Nagios exit codes
const (
	OK       = 0
	WARNING  = 1
	CRITICAL = 2
	UNKNOWN  = 3
)

// Default timeout (matching Nagios plugin conventions)
const defaultTimeout = 15 * time.Second

var (
	help     = pflag.BoolP("help", "h", false, "Show this help")
	version  = pflag.BoolP("version", "V", false, "Print version information.")
	verbose  = pflag.BoolP("verbose", "v", false, "Set logging to verbose level (use caution, may expose credentials)")
	server   = pflag.StringP("hostname", "H", "", "Host to SSH into")
	port     = pflag.IntP("port", "P", 1433, "Port")
	username = pflag.StringP("user", "u", "", "Username to connect with")
	password = pflag.StringP("pass", "p", "", "Password to connect with")
	credfile = pflag.StringP("credfile", "f", "", "Credentials file (format: username=<user>\npassword=<pass>)")
	database = pflag.StringP("database", "d", "", "Database name")
	timeout  = pflag.IntP("timeout", "t", int(defaultTimeout.Seconds()), "Timeout in seconds")
	query    = pflag.StringP("query", "q", "", "Query to execute")
	regex    = pflag.StringP("regex", "r", "", "Regex pattern to match against output")
)

func init() {
	pflag.Usage = func() {
		fmt.Printf(`check_mssql - Runs a query against an MS-SQL server and returns the first row
Returns CRITICAL if regex matches or errors occur. Row is passed to perfdata in semicolon-delimited format.
A simple SQL statement like "SELECT GETDATE()" verifies server responsiveness.

Syntax: check_mssql -H <server> -u <username> -p <password> -q <query> [-d <database>] [-P <port>] [-t <timeout>] [-r <regex>] [-v] [-h] [-V]

`)
		fmt.Println("Parameters:")
		pflag.PrintDefaults()
	}
}

func main() {
	// Preprocess arguments to handle -t10, etc.
	os.Args = append([]string{os.Args[0]}, preprocessArgs(os.Args[1:])...)

	// Parse flags
	pflag.Parse()

	if *version {
		printVersion()
		os.Exit(OK)
	}

	if *help {
		pflag.Usage()
		os.Exit(OK)
	}

	// Load credentials from file if provided
	if *credfile != "" {
		var err error
		*username, *password, err = loadCredentials(*credfile)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			pflag.Usage()
			os.Exit(UNKNOWN)
		}
	}

	// Validate required flags
	if *server == "" || *username == "" || *password == "" || *query == "" {
		fmt.Println("Error: Missing required arguments (server, username, password, query)")
		pflag.Usage()
		os.Exit(UNKNOWN)
	}

	config.AppConfig = config.Config{
		Verbose:  *verbose,
		Server:   *server,
		Port:     *port,
		Username: *username,
		Password: *password,
		Database: *database,
		Timeout:  *timeout,
		Query:    *query,
		Regex:    *regex,
	}

	logger.Config()

	// Set up timeout
	timeoutChan := time.After(time.Duration(*timeout) * time.Second)
	errChan := make(chan error, 1)
	resultChan := make(chan string, 1)

	// Run check in a goroutine to handle timeout
	go func() {
		result, err := runQuery()
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- result
	}()

	// Wait for result, error, or timeout
	select {
	case result := <-resultChan:
		// Check regex if provided (only for queries)
		if (config.AppConfig.Query != "") && (config.AppConfig.Regex != "") {
			matched, err := regexp.MatchString(config.AppConfig.Regex, result)
			if err != nil {
				fmt.Printf("SQL CRITICAL: Invalid regex: %v\n", err)
				os.Exit(CRITICAL)
			}
			if matched {
				fmt.Printf("SQL CRITICAL: %s|%s\n", result, result)
				os.Exit(CRITICAL)
			}
		}
		fmt.Printf("SQL OK: %s|%s\n", result, result)
		os.Exit(OK)
	case err := <-errChan:
		fmt.Printf("SQL CRITICAL: %v\n", err)
		os.Exit(CRITICAL)
	case <-timeoutChan:
		fmt.Printf("SQL UNKNOWN: ERROR connection %s (timeout after %v)\n", config.AppConfig.Server, config.AppConfig.Timeout)
		os.Exit(UNKNOWN)
	}
}

func runQuery() (string, error) {
	// Build connection string
	connString := fmt.Sprintf("server=%s;port=%d;user id=%s;password=%s;connection timeout=%d",
		*server, *port, *username, *password, *timeout)
	if *database != "" {
		connString += fmt.Sprintf(";database=%s", *database)
	}
	logger.Log.Debug("connecting to database",
		zap.String("connString", connString),
	)

	// Connect to database
	db, err := sql.Open("sqlserver", connString)
	if err != nil {
		logger.Log.Warn("failed to run command",
			zap.Error(err),
		)
		return "", fmt.Errorf("can't connect to server: %v", err)
	}
	defer db.Close()

	// Set connection timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(config.AppConfig.Timeout)*time.Second)
	defer cancel()

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		logger.Log.Warn("can't ping server",
			zap.Error(err),
		)
		return "", fmt.Errorf("can't ping server: %v", err)
	}

	// Execute query
	rows, err := db.QueryContext(ctx, config.AppConfig.Query)
	if err != nil {
		logger.Log.Warn("query error",
			zap.Error(err),
		)
		return "", fmt.Errorf("query error: %v", err)
	}
	defer rows.Close()

	// Fetch first row
	if !rows.Next() {
		logger.Log.Warn("query returned no rows",
			zap.Error(err),
		)
		return "", fmt.Errorf("query returned no rows")
	}

	// Get column values
	cols, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("error getting columns: %v", err)
	}

	// Scan row into interface slice
	values := make([]interface{}, len(cols))
	valuePtrs := make([]interface{}, len(cols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}
	if err := rows.Scan(valuePtrs...); err != nil {
		return "", fmt.Errorf("error scanning row: %v", err)
	}

	// Convert values to strings and join with semicolon
	var result []string
	for _, val := range values {
		switch v := val.(type) {
		case nil:
			result = append(result, "")
		case []byte:
			result = append(result, string(v))
		default:
			result = append(result, fmt.Sprintf("%v", v))
		}
	}
	return strings.Join(result, ";"), nil
}

func loadCredentials(filename string) (string, string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", "", fmt.Errorf("failed to read credentials file: %v", err)
	}

	creds := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		creds[key] = value
	}

	username, ok := creds["username"]
	if !ok {
		return "", "", fmt.Errorf("credentials file missing username")
	}
	password, ok := creds["password"]
	if !ok {
		return "", "", fmt.Errorf("credentials file missing password")
	}
	return username, password, nil
}

func printVersion() {
	fmt.Println("check_mssql")
	fmt.Printf("Version: %s\n", VERSION)
	fmt.Println("Author(s): slashdoom (Patrick Ryon)")
	fmt.Println("Nagios check for MS SQL Server")
}

func preprocessArgs(args []string) []string {
	var newArgs []string
	for _, arg := range args {
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' && !strings.Contains(arg, "=") {
			flagName := arg[:2]
			flagValue := arg[2:]
			newArgs = append(newArgs, flagName, flagValue)
		} else {
			newArgs = append(newArgs, arg)
		}
	}
	return newArgs
}

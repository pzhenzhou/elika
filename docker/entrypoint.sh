#!/bin/sh
# Function to parse command line arguments
parse_args() {
  # Get Pod ID from hostname
  local pod_id=$(hostname)
  local namespace=${MY_NAMESPACE}

  while [ "$#" -gt 0 ]; do
    case $1 in
    # Basic configuration
    --port)
      PORT="$2"
      shift
      ;;
    --service-port)
      SERVICE_PORT="$2"
      shift
      ;;
    --multi-core)
      MULTI_CORE="$2"
      shift
      ;;
    --core-num)
      CORE_NUM="$2"
      shift
      ;;
    --enable-tls)
      ENABLE_TLS="$2"
      shift
      ;;
    --enable-cluster-cache)
      ENABLE_CONN_CACHE="$2"
      shift
      ;;
    --trace-active-user)
      TRACE_ACTIVE_USER="$2"
      shift
      ;;
    # Backend pool configuration
    --backend-pool.max-size)
      BACKEND_POOL_MAX_SIZE="$2"
      shift
      ;;
    --backend-pool.max-idle)
      BACKEND_POOL_MAX_IDLE="$2"
      shift
      ;;
    # Router configuration
    --router.balancer)
      ROUTE_BALANCER="$2"
      shift
      ;;
    --router.type)
      ROUTER_TYPE="$2"
      shift
      ;;
    --router.static-cluster)
      ROUTER_STATIC_BE="$2"
      shift
      ;;
    --router.cp-addr)
      ROUTER_CP_ADDR="$2"
      shift
      ;;
    # Web proxy configuration
    --web-proxy.pprof)
      WEB_SERVER_PPROF="$2"
      shift
      ;;
    *)
      echo "Unknown parameter passed: $1"
      exit 1
      ;;
    esac
    shift
  done

  # Build command string
  local cmd="./elika-proxy-srv --node.id=$pod_id --node.namespace=$namespace$"

  # Basic configuration
  [ -n "$PORT" ] && cmd="$cmd --port=$PORT"
  [ -n "$SERVICE_PORT" ] && cmd="$cmd --service-port=$SERVICE_PORT"
  [ -n "$MULTI_CORE" ] && cmd="$cmd --multi-core=$MULTI_CORE"
  [ -n "$CORE_NUM" ] && cmd="$cmd --core-num=$CORE_NUM"
  [ -n "$ENABLE_TLS" ] && cmd="$cmd --enable-tls=$ENABLE_TLS"
  [ -n "$ENABLE_CONN_CACHE" ] && cmd="$cmd --enable-conn-cache=$ENABLE_CONN_CACHE"
  [ -n "$TRACE_ACTIVE_USER" ] && cmd="$cmd --trace-active-user=$TRACE_ACTIVE_USER"

  # Backend pool configuration
  [ -n "$BACKEND_POOL_MAX_SIZE" ] && cmd="$cmd --backend-pool.max-size=$BACKEND_POOL_MAX_SIZE"
  [ -n "$BACKEND_POOL_MAX_IDLE" ] && cmd="$cmd --backend-pool.max-idle=$BACKEND_POOL_MAX_IDLE"

  # Router configuration
  [ -n "$ROUTE_BALANCER" ] && cmd="$cmd --router.balancer=$ROUTE_BALANCER"
  [ -n "$ROUTER_TYPE" ] && cmd="$cmd --router.type=$ROUTER_TYPE"
  [ -n "$ROUTER_STATIC_BE" ] && cmd="$cmd --router.static-be=$ROUTER_STATIC_BE"
  [ -n "$ROUTER_CP_ADDR" ] && cmd="$cmd --router.cp-addr=$ROUTER_CP_ADDR"

  # Web proxy configuration
  [ -n "$WEB_SERVER_PPROF" ] && cmd="$cmd --web-server.pprof=$WEB_SERVER_PPROF"

  echo "$cmd"
}

# Main function
main() {
  local cmd
  cmd=$(parse_args "$@")
  echo "Executing: $cmd"
  eval "$cmd"
}

main "$@"

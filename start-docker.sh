#!/bin/sh

die() {
    echo "$2" >&2
    exit $1
}

set -e

# If the hub config file doesn't exist, initialize the hub
if [ ! -e "hub.yml" ]; then
    echo "--- Starting GoHub setup ---"
    echo

    go-hub init || \
        die $? "Failed to initialize hub"

    # Default admin user/pass (don't override if already set)
    ADMIN=${ADMIN_USER:-admin}
    PASS=${ADMIN_PASSWORD:-$(pwgen -s 12 1)}

    go-hub user add "${ADMIN}" "${PASS}" root || \
        die $? "Failed to create admin user"

    printf '%s\n' \
        "Username: ${ADMIN}" \
        "Password: ${PASS}" \
        "" \
        "To add another admin, run the following (container must be running):" \
        "docker exec container_name \\" \
        "  go-hub user create \"new-admin\" \"new-password\" root" \
    | tee admin.txt

    echo
    echo "--- Finished GoHub setup ---"
    echo
fi

exec go-hub serve "$@" || \
    die $? "Failed to start go-hub: $?"

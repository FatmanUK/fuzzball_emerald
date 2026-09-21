
$ export NETWORK='fuzzball'
$ export PG_CONTAINER='fuzzball-db'
$ podman network exists $NETWORK || podman network create $NETWORK
$ podman rm -f $PG_CONTAINER

$ export DB_PORT='15432'
$ export DB_USER='emerald'
$ export DB_PASSWORD=$(sha256sum <(date +%Y%m%d%H%M%S) | cut -b -20)
$ export DB_NAME='fuzzball_emerald'
$ export DB_IMAGE='docker.io/library/postgres:17-alpine'
$ podman run -d --name $PG_CONTAINER --network $NETWORK \
	-e POSTGRES_USER=$DB_USER \
	-e POSTGRES_PASSWORD=$DB_PASSWORD \
	-e POSTGRES_DB=$DB_NAME \
	-p $DB_PORT:5432 \
	-v fbemerald-pgdata:/var/lib/postgresql/data \
	$DB_IMAGE >/dev/null
$ podman exec $PG_CONTAINER pg_isready -U $DB_USER

$ export TLS_DIR='/etc/emerald'
$ export CERT_FILE=$TLS_DIR/cert.pem
$ export KEY_FILE=$TLS_DIR/key.pem
$ export CERT_CN='IP:192.168.5.214'
$ export CERT_DAYS='365'
$ sudo install -d $TLS_DIR -m 0755 -o $(whoami) -g $(whoami)
$ openssl req -x509 -newkey rsa:4096 -nodes -days $CERT_DAYS \
	-subj "/CN=$CERT_CN" \
	-addext "subjectAltName=DNS:$CERT_CN,DNS:localhost,IP:127.0.0.1" \
	-keyout $KEY_FILE -out $CERT_FILE
$ chmod 600 $KEY_FILE

$ export APP_CONTAINER='fbemerald'
$ export NONROOT_UID='65532'
$ export USERNS="keep-id:uid=$NONROOT_UID,gid=$NONROOT_UID"
$ export LINE_PORT='14202'
$ export WSS_PORT='14203'
$ export DB_URL_POD="postgres://$DB_USER:$DB_PASSWORD@$PG_CONTAINER:$DB_PORT/$DB_NAME?sslmode=disable"
$ export DB_URL_POD="postgres://$DB_USER:$DB_PASSWORD@192.168.5.214:15432/$DB_NAME?sslmode=disable"
$ export IMAGE='ghcr.io/fatmanuk/fuzzball_emerald'
$ export TAG='latest'

$ export DB_URL_POD="postgres://$DB_USER:$DB_PASSWORD@192.168.5.214:$DB_PORT/$DB_NAME?sslmode=disable"

$ podman run --rm --network $NETWORK --userns=$USERNS -p $LINE_PORT:4202 -p $WSS_PORT:4203 -e FBE_DATABASE_URL="$DB_URL_POD" -v $TLS_DIR:/etc/fbemerald/tls:ro,z -v /opt/emerald/testdata/starterdb:/dump:ro,z $IMAGE:$TAG import -force /dump/starterdb.db

$ podman run -d --name $APP_CONTAINER --network $NETWORK --userns=$USERNS -p $LINE_PORT:4202 -p $WSS_PORT:4203 -e FBE_DATABASE_URL="$DB_URL_POD" -v $TLS_DIR:/etc/fbemerald/tls:ro,z $IMAGE:$TAG serve


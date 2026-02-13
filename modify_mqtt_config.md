
tee /etc/mosquitto/mosquitto.conf >/dev/null <<'EOF'
allow_anonymous false
password_file /etc/mosquitto/pwd
log_dest syslog
listener 1883
protocol mqtt
listener 18883
protocol mqtt
cafile /etc/ssl/certs/ca-mqtt.crt
certfile /etc/ssl/certs/server.crt
keyfile /etc/ssl/certs/server.key
require_certificate true
use_identity_as_username true
tls_version tlsv1.2
ciphers DEFAULT:!3DES:!RC4
EOF
/etc/init.d/mosquitto restart

#!/usr/bin/env sh

cd ../build/test && docker-compose build && docker-compose up -d

# BACK_PID=$!
# wait $BACK_PID

#../build/test/wait-for-it.sh 127.0.0.1:443 -- echo "=========================== SERVER IS UP ===================================="




cd ../../tests && jest

cd ../build/test && docker-compose down

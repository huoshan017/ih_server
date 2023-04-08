#!/bin/bash
go build  -o ../bin/login_server ih_server/src/login_server
go build  -o ../bin/hall_server ih_server/src/hall_server
go build  -o ../bin/rpc_server ih_server/src/rpc_server
go build  -o ../bin/test_client ih_server/src/test_client
go build  -o ../bin/table_generator ih_server/src/table_generator

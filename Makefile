gen-update-package:
	go get github.com/magic-lib/go-plat-trace@master
	go get github.com/magic-lib/go-plat-cache@master
	go get github.com/magic-lib/go-plat-curl@master
	go get github.com/magic-lib/go-plat-mysql@master
	go get github.com/magic-lib/go-plat-retry@master
	go get github.com/magic-lib/go-plat-startupcfg@master
	go get github.com/magic-lib/go-plat-utils@master
	go mod tidy

build:
	go build -o ./workflow/web/cmd/workflow ./workflow/web/cmd/
	./workflow/web/cmd/workflow -dsn="root:mjhttyryt565-jyjh5824t-p55w@tcp(202.60.228.31:20366)/rule-workflow?charset=utf8mb4&parseTime=True&loc=Local"

run:
	go run ./workflow/web/cmd/
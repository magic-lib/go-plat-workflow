gen-update-package:
	go get github.com/magic-lib/go-plat-trace@master
	go get github.com/magic-lib/go-plat-cache@master
	go get github.com/magic-lib/go-plat-curl@master
	go get github.com/magic-lib/go-plat-mysql@master
	go get github.com/magic-lib/go-plat-mq@master
	go get github.com/magic-lib/go-plat-retry@master
	go get github.com/magic-lib/go-plat-startupcfg@master
	go get github.com/magic-lib/go-plat-utils@master
	go mod tidy

build:
	go build -o /Volumes/MacintoshData/workspace/goland-framework/code/magic-lib/web/workflow ./workflow/web/cmd/
	/Volumes/MacintoshData/workspace/goland-framework/code/magic-lib/web/workflow

run:
	go run ./workflow/web/cmd/
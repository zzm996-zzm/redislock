.PHONY: e2e
e2e:
	sh ./script/integrate_test.sh

.PHONY: e2e_up
e2e_up:
	docker compose -f script/docker-compose.yml up -d

.PHONY: e2e_down
e2e_down:
	docker compose -f script/docker-compose.yml down

.PHONY: mock
mock:
	mockgen -package=mocks -destination=./mocks/reids_cmdable.mock.go  github.com/redis/go-redis/v9 Cmdable
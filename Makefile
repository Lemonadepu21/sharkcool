.PHONY: build driver desktop native-test test check release
build: driver desktop
driver:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags='-s -w' -o dist/sharkcool ./cmd/sharkcool
desktop:
	cmake -S desktop -B dist/native-build -DCMAKE_BUILD_TYPE=Release -DBUILD_TESTING=OFF
	cmake --build dist/native-build --target sharkcool-window -j2
	install -m755 dist/native-build/sharkcool-window dist/sharkcool-window
native-test:
	cmake -S desktop -B dist/native-build -DCMAKE_BUILD_TYPE=Release -DBUILD_TESTING=ON
	cmake --build dist/native-build --target sharkcool-window-tests -j2
	ctest --test-dir dist/native-build --output-on-failure
test:
	go test -race ./...
	node internal/webui/tests/app.test.cjs
	$(MAKE) native-test
check:
	go vet ./...
	node --check internal/webui/assets/app.js
	sh -n scripts/install.sh scripts/release.sh
release: test check
	sh scripts/release.sh

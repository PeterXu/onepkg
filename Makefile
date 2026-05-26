
build:
	cd cmd/onepkg && go build -o onepkg .

install: build
	@mkdir -p $(HOME)/.local/bin
	cp -f cmd/onepkg/onepkg $(HOME)/.local/bin

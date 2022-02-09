run:; go run .

build:; go build .; \
    rm -rf nervos.app; \
    mkdir -p nervos.app/Contents/MacOS; \
    mv nervos nervos.app/Contents/MacOS; \
    mkdir -p nervos.app/Contents/Resources; \
    cp support/icon.icns nervos.app/Contents/Resources; \
    cp support/info.plist nervos.app/Contents/Info.plist

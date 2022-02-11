run:; go run .

build:; go build .; \
    rm -rf nervos.app; \
    mkdir -p nervos.app/Contents/MacOS; \
    mv nervos nervos.app/Contents/MacOS; \
    mkdir -p nervos.app/Contents/Resources; \
    cp support/icon.icns nervos.app/Contents/Resources; \
    cp support/info.plist nervos.app/Contents/Info.plist

buildios:; gogio -target ios -appid com.atriumph.nervos -icon support/icon.png . && \
	  rm -rf Payload nervosios.app && \
	  unzip nervos.ipa && \
	  mv Payload/nervos.app nervosios.app && \
	  rm -rf Payload nervos.ipa && \
		chmod +x nervosios.app/Nervos
installiossim:; gogio -target ios -appid com.atriumph.nervos -icon support/icon.png -arch amd64 -o nervosios.app . && \
    xcrun simctl install booted nervosios.app

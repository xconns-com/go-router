include $(GOROOT)/src/Make.inc

all:	install

install:
	cd trunk && make install
	cd branches/router1 && make install

clean:
	cd trunk && make clean
	cd branches/router1 && make clean

nuke:
	cd trunk && make nuke
	cd branches/router1 && make nuke

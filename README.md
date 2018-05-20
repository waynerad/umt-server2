# umt-server2
UMT external instrument server

UMT server2 is an external instrument server for UMT (Ultimate Music Toy), the interactive algorithmic music composition system.

Currently, because I've been unable to get portmidi to install on my Mac, it doesn't support MIDI, only lights. It supports two light systems, Dan Julio's lighting system which I call "Dan Lights", and the FadeCandy system (which was also set up by Dan Julio, though in the source code it is called "FadeCandy").

The system works by having the client open a web sockets connection. Once the connection is open, timing codes are sent along with notes to play to synchronize playback between the client (which uses Web Audio) and the external instruments controlled by this server.

An "instrument" interface is defined in Go so new instruments can be added just by writing the functions that fulfill the interface.

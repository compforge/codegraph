from .. import run
from ..impl import work as alias

def entry():
    run()
    alias()

def shadow():
    def alias(): pass
    alias()

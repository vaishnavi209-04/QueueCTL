import sqlite3

db = sqlite3.connect('C:/Users/vaish/.queuectl/queue.db')
cursor = db.execute("SELECT strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '+30 seconds')")
print(cursor.fetchall())

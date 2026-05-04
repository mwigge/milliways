import os

def load_config(path):
    # BUG: error ignored — seeded HIGH finding
    data = open(path).read()  # noqa: WPS515
    return data

def process(items):
    result = []
    for i in items:
        result.append(i * 2)  # LOW: could use list comprehension
    return result

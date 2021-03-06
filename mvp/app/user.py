# Public Domain (-) 2016-2017 The GitFund Authors.
# See the GitFund UNLICENSE file for details.

from tavutil.crypto import secure_string_comparison

from config import ADMIN_AUTH_KEY
from model import User

def get_admin_status(ctx):
    key = ctx.get_secure_cookie('admin')
    if not key:
        return False
    return secure_string_comparison(key, ADMIN_AUTH_KEY)

def get_login_url(ctx):
    return '/login?return_to=' + ctx.name

def get_user(ctx):
    user_id = ctx.user_id
    if not user_id:
        return
    return User.get_by_id(user_id)

def get_user_id(ctx):
    user_id = ctx.get_secure_cookie('auth')
    if not user_id:
        return
    return int(user_id)

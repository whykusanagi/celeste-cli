class Account < Base
  def initialize
    @balance = 0
  end

  def deposit(amount)
    add_amount(amount)
    log_event "deposit"
  end

  def add_amount(amount)
    @balance + amount
  end

  def log_event(msg)
    puts msg
  end

  def withdraw(amount)
    # TODO: implement withdraw
  end
end

def dead_rb
end

def run
  Account.new.deposit(5)
end
